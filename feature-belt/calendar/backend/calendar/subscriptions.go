package calendar

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
)

// Busy time from subscribed calendars is kept for this window around now.
const (
	subPast   = 7 * 24 * time.Hour
	subAhead  = 180 * 24 * time.Hour
	subBlocks = 5000
)

// Subscription is an external calendar added by its secret iCal address.
// The address itself is never returned: only its host.
type Subscription struct {
	ID            uuid.UUID  `json:"id"`
	Name          string     `json:"name"`
	Host          string     `json:"host"`
	Color         string     `json:"color"`
	Enabled       bool       `json:"enabled"`
	LastFetchedAt *time.Time `json:"last_fetched_at"`
	LastSuccessAt *time.Time `json:"last_success_at"`
	LastError     string     `json:"last_error"`
	BusyCount     int        `json:"busy_count"`
	CreatedAt     time.Time  `json:"created_at"`
}

const subCols = `s.id, s.name, s.url_host, s.color, s.enabled, s.last_fetched_at, s.last_success_at, s.last_error,
	(SELECT count(*) FROM calendar_busy_blocks b WHERE b.subscription_id = s.id AND upper(b.span) > now())::int, s.created_at`

func scanSub(row pgx.Row) (Subscription, error) {
	var s Subscription
	err := row.Scan(&s.ID, &s.Name, &s.Host, &s.Color, &s.Enabled, &s.LastFetchedAt, &s.LastSuccessAt, &s.LastError, &s.BusyCount, &s.CreatedAt)
	return s, err
}

var errSubsOff = httpx.NewError(http.StatusConflict, "subscriptions_unavailable", "Adding other calendars isn't set up in this app.")

func (s *Service) listSubscriptions(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	rows, err := s.d.Pool.Query(r.Context(), `SELECT `+subCols+` FROM calendar_subscriptions s WHERE s.user_id = $1 ORDER BY s.created_at, s.id`, id.UserID)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := []Subscription{}
	for rows.Next() {
		sub, err := scanSub(rows)
		if err != nil {
			return err
		}
		out = append(out, sub)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.OK(w, map[string]any{"items": out})
	return nil
}

// fetchResult is one fetch of a subscribed calendar.
type fetchResult struct {
	spans        []busySpan
	etag         string
	lastModified string
	notModified  bool
}

// fetchErr carries a sentence for the person who added the calendar.
type fetchErr struct {
	msg string
	err error
}

func (e *fetchErr) Error() string { return e.msg + ": " + e.err.Error() }

func userFetchError(err error) string {
	var fe *fetchErr
	if errors.As(err, &fe) {
		return fe.msg
	}
	return "Couldn't read that calendar. Check the address and try again."
}

// fetchFeed downloads and reads a calendar through the guarded client.
func (s *Service) fetchFeed(ctx context.Context, raw, etag, lastModified string, loc *time.Location) (fetchResult, error) {
	u, err := checkFeedURL(raw, s.allowPrivate)
	if err != nil {
		if errors.Is(err, errBlockedHost) {
			return fetchResult{}, &fetchErr{"That address points to a private network. Use the calendar's public secret address.", err}
		}
		return fetchResult{}, &fetchErr{"That isn't a calendar address. Paste the secret iCal (.ics) link.", err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fetchResult{}, &fetchErr{"That isn't a calendar address. Paste the secret iCal (.ics) link.", err}
	}
	req.Header.Set("Accept", "text/calendar, text/plain;q=0.5, */*;q=0.1")
	req.Header.Set("User-Agent", "ScaffoldApp-Calendar/1.0")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}
	res, err := s.fetcher.Do(req)
	if err != nil {
		switch {
		case errors.Is(err, errBlockedHost):
			return fetchResult{}, &fetchErr{"That address points to a private network. Use the calendar's public secret address.", err}
		case errors.Is(err, context.DeadlineExceeded) || isTimeout(err):
			return fetchResult{}, &fetchErr{"The calendar took too long to answer. Try again later.", err}
		}
		return fetchResult{}, &fetchErr{"Couldn't reach that calendar address.", err}
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotModified {
		return fetchResult{notModified: true, etag: etag, lastModified: lastModified}, nil
	}
	if res.StatusCode != http.StatusOK {
		return fetchResult{}, &fetchErr{"The calendar address answered " + strconv.Itoa(res.StatusCode) + ". Check that the link is still shared.", errors.New(res.Status)}
	}
	if res.ContentLength > s.subMaxBytes {
		return fetchResult{}, &fetchErr{"That calendar is larger than " + strconv.FormatInt(s.subMaxBytes>>20, 10) + " MB.", errTooLarge}
	}
	body, err := readLimited(res.Body, s.subMaxBytes)
	if err != nil {
		if errors.Is(err, errTooLarge) {
			return fetchResult{}, &fetchErr{"That calendar is larger than " + strconv.FormatInt(s.subMaxBytes>>20, 10) + " MB.", err}
		}
		return fetchResult{}, &fetchErr{"The calendar stopped sending before it finished. Try again.", err}
	}
	now := time.Now().UTC()
	spans, err := icalBusy(body, loc, now.Add(-subPast), now.Add(subAhead), subBlocks)
	if err != nil {
		return fetchResult{}, &fetchErr{"That address didn't return a calendar file.", err}
	}
	return fetchResult{spans: spans, etag: res.Header.Get("ETag"), lastModified: res.Header.Get("Last-Modified")}, nil
}

func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}

func replaceBlocks(ctx context.Context, q db.Querier, subID, userID uuid.UUID, spans []busySpan) error {
	if _, err := q.Exec(ctx, `DELETE FROM calendar_busy_blocks WHERE subscription_id = $1`, subID); err != nil {
		return err
	}
	if len(spans) == 0 {
		return nil
	}
	starts := make([]time.Time, len(spans))
	ends := make([]time.Time, len(spans))
	for i, sp := range spans {
		starts[i], ends[i] = sp.Start, sp.End
	}
	_, err := q.Exec(ctx, `
		INSERT INTO calendar_busy_blocks (subscription_id, user_id, span)
		SELECT $1, $2, tstzrange(a, b) FROM unnest($3::timestamptz[], $4::timestamptz[]) AS t(a, b)`, subID, userID, starts, ends)
	return err
}

func (s *Service) createSubscription(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	if s.secretKey == nil {
		return errSubsOff
	}
	var in struct {
		Name  string `json:"name"`
		URL   string `json:"url"`
		Color string `json:"color"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	name, ok := cleanText(in.Name, 80)
	if name == "" || !ok {
		return httpx.BadRequest("Name the calendar (up to 80 characters).")
	}
	if in.Color == "" {
		in.Color = "neutral"
	}
	if !validColor(in.Color) {
		return httpx.BadRequest("Pick one of the calendar colors.")
	}
	var count int
	if err := s.d.Pool.QueryRow(r.Context(), `SELECT count(*) FROM calendar_subscriptions WHERE user_id = $1`, id.UserID).Scan(&count); err != nil {
		return err
	}
	if count >= maxSubs {
		return httpx.Conflict("You can add up to 20 calendars.")
	}
	u, err := checkFeedURL(in.URL, s.allowPrivate)
	if err != nil {
		if errors.Is(err, errBlockedHost) {
			return httpx.BadRequest("That address points to a private network. Use the calendar's public secret address.")
		}
		return httpx.BadRequest("That isn't a calendar address. Paste the secret iCal (.ics) link.")
	}
	loc := s.zoneFor(r.Context(), s.d.Pool, id.UserID, time.UTC)
	res, err := s.fetchFeed(r.Context(), u.String(), "", "", loc)
	if err != nil {
		return httpx.BadRequest(userFetchError(err))
	}
	sealed, err := s.seal(u.String())
	if err != nil {
		return err
	}
	var subID uuid.UUID
	err = db.WithTx(r.Context(), s.d.Pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO calendar_subscriptions (user_id, name, url_enc, url_host, color, etag, last_modified,
			  last_fetched_at, last_success_at, next_fetch_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now(), now() + make_interval(secs => $8)) RETURNING id`,
			id.UserID, name, sealed, u.Hostname(), in.Color, res.etag, res.lastModified, s.subInterval.Seconds()).Scan(&subID); err != nil {
			return err
		}
		return replaceBlocks(r.Context(), tx, subID, id.UserID, res.spans)
	})
	if err != nil {
		return err
	}
	sub, err := scanSub(s.d.Pool.QueryRow(r.Context(), `SELECT `+subCols+` FROM calendar_subscriptions s WHERE s.id = $1`, subID))
	if err != nil {
		return err
	}
	s.changed(r.Context(), "subscriptions", id.UserID)
	httpx.JSON(w, http.StatusCreated, sub)
	return nil
}

func (s *Service) loadOwnSub(ctx context.Context, userID, id uuid.UUID) (Subscription, error) {
	sub, err := scanSub(s.d.Pool.QueryRow(ctx, `SELECT `+subCols+` FROM calendar_subscriptions s WHERE s.id = $1 AND s.user_id = $2`, id, userID))
	if db.IsNoRows(err) {
		return sub, httpx.NotFound("That calendar isn't in your list.")
	}
	return sub, err
}

func (s *Service) updateSubscription(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	sid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		Name    *string `json:"name"`
		Color   *string `json:"color"`
		Enabled *bool   `json:"enabled"`
	}
	if err := httpx.Decode(r, &in); err != nil {
		return err
	}
	sub, err := s.loadOwnSub(r.Context(), id.UserID, sid)
	if err != nil {
		return err
	}
	if in.Name != nil {
		name, ok := cleanText(*in.Name, 80)
		if name == "" || !ok {
			return httpx.BadRequest("Name the calendar (up to 80 characters).")
		}
		sub.Name = name
	}
	if in.Color != nil {
		if !validColor(*in.Color) {
			return httpx.BadRequest("Pick one of the calendar colors.")
		}
		sub.Color = *in.Color
	}
	if in.Enabled != nil {
		sub.Enabled = *in.Enabled
	}
	if _, err := s.d.Pool.Exec(r.Context(), `UPDATE calendar_subscriptions SET name = $3, color = $4, enabled = $5,
		next_fetch_at = CASE WHEN $5 AND NOT enabled THEN now() ELSE next_fetch_at END, updated_at = now()
		WHERE id = $1 AND user_id = $2`, sid, id.UserID, sub.Name, sub.Color, sub.Enabled); err != nil {
		return err
	}
	out, err := s.loadOwnSub(r.Context(), id.UserID, sid)
	if err != nil {
		return err
	}
	s.changed(r.Context(), "subscriptions", id.UserID)
	httpx.OK(w, out)
	return nil
}

func (s *Service) deleteSubscription(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	sid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	tag, err := s.d.Pool.Exec(r.Context(), `DELETE FROM calendar_subscriptions WHERE id = $1 AND user_id = $2`, sid, id.UserID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return httpx.NotFound("That calendar isn't in your list.")
	}
	s.changed(r.Context(), "subscriptions", id.UserID)
	httpx.NoContent(w)
	return nil
}

func (s *Service) refreshSubscription(w http.ResponseWriter, r *http.Request) error {
	id := auth.MustFromContext(r.Context())
	sid, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	if _, err := s.loadOwnSub(r.Context(), id.UserID, sid); err != nil {
		return err
	}
	if err := s.refreshOne(r.Context(), sid, true); err != nil {
		return err
	}
	out, err := s.loadOwnSub(r.Context(), id.UserID, sid)
	if err != nil {
		return err
	}
	s.changed(r.Context(), "subscriptions", id.UserID)
	httpx.OK(w, out)
	return nil
}

// refreshDue re-fetches subscriptions whose time has come (worker).
func (s *Service) refreshDue(ctx context.Context) error {
	if s.secretKey == nil {
		return nil
	}
	rows, err := s.d.Pool.Query(ctx, `SELECT id FROM calendar_subscriptions WHERE enabled AND next_fetch_at <= now()
		ORDER BY next_fetch_at LIMIT 20`)
	if err != nil {
		return err
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.refreshOne(ctx, id, false); err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, "calendar: subscription refresh failed", "subscription_id", id, "err", err)
		}
	}
	return nil
}

// refreshOne fetches one subscription and stores the outcome: new busy
// blocks, not modified, or the error with exponential backoff. force skips
// the conditional request.
func (s *Service) refreshOne(ctx context.Context, id uuid.UUID, force bool) error {
	var userID uuid.UUID
	var sealed []byte
	var etag, lastMod string
	var failures int
	var stale bool
	if err := s.d.Pool.QueryRow(ctx, `SELECT user_id, url_enc, etag, last_modified, failures,
		COALESCE(last_success_at < now() - interval '24 hours', true) FROM calendar_subscriptions WHERE id = $1`, id).
		Scan(&userID, &sealed, &etag, &lastMod, &failures, &stale); err != nil {
		return err
	}
	raw, err := s.open(sealed)
	if err != nil {
		return err
	}
	if force || stale {
		// A full read at least daily keeps the busy window moving forward.
		etag, lastMod = "", ""
	}
	loc := s.zoneFor(ctx, s.d.Pool, userID, time.UTC)
	res, ferr := s.fetchFeed(ctx, raw, etag, lastMod, loc)
	if ferr != nil {
		backoff := min(s.subInterval<<min(failures, 6), 24*time.Hour)
		_, err := s.d.Pool.Exec(ctx, `UPDATE calendar_subscriptions SET last_fetched_at = now(), last_error = $2,
			failures = failures + 1, next_fetch_at = now() + make_interval(secs => $3), updated_at = now() WHERE id = $1`,
			id, userFetchError(ferr), backoff.Seconds())
		return err
	}
	return db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if !res.notModified {
			if err := replaceBlocks(ctx, tx, id, userID, res.spans); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE calendar_subscriptions SET last_fetched_at = now(), last_success_at = now(),
			last_error = '', failures = 0, etag = $2, last_modified = $3,
			next_fetch_at = now() + make_interval(secs => $4), updated_at = now() WHERE id = $1`,
			id, res.etag, res.lastModified, s.subInterval.Seconds())
		return err
	})
}
