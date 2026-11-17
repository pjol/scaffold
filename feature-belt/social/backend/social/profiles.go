package social

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
)

// Link is one profile link.
type Link struct {
	URL   string `json:"url"`
	Label string `json:"label"` // "" shows the host
}

// ProfileViewer is how the caller relates to a profile. Whether the caller
// blocked them is moderation's (GET /v1/moderation/blocks/ids); who blocked
// the caller is never said.
type ProfileViewer struct {
	IsSelf     bool `json:"is_self"`
	Following  bool `json:"following"`   // the caller follows them
	FollowsYou bool `json:"follows_you"` // they follow the caller
}

// Profile is GET /v1/social/profiles/{handle}.
type Profile struct {
	User      Ref       `json:"user"`
	Bio       string    `json:"bio"`
	Links     []Link    `json:"links"`
	JoinedAt  time.Time `json:"joined_at"`
	Followers int       `json:"followers"`
	Following int       `json:"following"`
	// Sections says, per registered section, whether the caller may see it.
	Sections map[string]bool `json:"sections"`
	// Restricted: a block separates the caller and the owner; only the name
	// and photo are shown.
	Restricted bool          `json:"restricted"`
	Viewer     ProfileViewer `json:"viewer"`
}

// PrivacyRow is one section in the caller's privacy settings.
type PrivacyRow struct {
	Section
	Visibility string `json:"visibility"`
}

// MyProfile is GET /v1/social/me: what the caller edits.
type MyProfile struct {
	User            Ref          `json:"user"`
	Bio             string       `json:"bio"`
	Links           []Link       `json:"links"`
	Privacy         []PrivacyRow `json:"privacy"`
	HandleChangedAt *time.Time   `json:"handle_changed_at"`
	// HandleChangeAt is when the handle may next change; null: now.
	HandleChangeAt *time.Time `json:"handle_change_at"`
	Limits         struct {
		BioMaxChars         int `json:"bio_max_chars"`
		MaxLinks            int `json:"max_links"`
		HandleCooldownHours int `json:"handle_cooldown_hours"` // between changes (0 = any time)
		HandleRedirectHours int `json:"handle_redirect_hours"` // how long an old handle redirects
	} `json:"limits"`
}

type profileRow struct {
	ref                  Ref
	bio                  string
	links                []Link
	privacy              Privacy
	followers, following int
	status               string
	joined               time.Time
	handleChanged        *time.Time
}

func loadProfile(ctx context.Context, q db.Querier, id uuid.UUID) (profileRow, error) {
	if err := ensureProfiles(ctx, q, []uuid.UUID{id}); err != nil {
		return profileRow{}, err
	}
	var p profileRow
	var links, privacy []byte
	err := q.QueryRow(ctx, `
		SELECT u.id, p.handle, u.display_name, u.avatar_url, p.bio, p.links, p.privacy, p.followers, p.following,
		       u.status, u.created_at, p.handle_changed_at
		FROM users u JOIN social_profiles p ON p.user_id = u.id WHERE u.id = $1`, id).
		Scan(&p.ref.UserID, &p.ref.Handle, &p.ref.DisplayName, &p.ref.AvatarURL, &p.bio, &links, &privacy,
			&p.followers, &p.following, &p.status, &p.joined, &p.handleChanged)
	if isNoRows(err) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	p.links = parseLinks(links)
	p.privacy = parsePrivacy(privacy)
	return p, nil
}

func parseLinks(raw []byte) []Link {
	out := []Link{}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = []Link{}
	}
	return out
}

// BuildProfile is owner's profile as viewer sees it (uuid.Nil: an anonymous
// reader, who sees public sections only).
func BuildProfile(ctx context.Context, q db.Querier, ownerID, viewerID uuid.UUID) (Profile, error) {
	row, err := loadProfile(ctx, q, ownerID)
	if err != nil {
		return Profile{}, err
	}
	if row.status != "active" && row.status != "suspended" && ownerID != viewerID {
		return Profile{}, ErrNotFound
	}
	v, err := viewerOf(ctx, q, ownerID, viewerID)
	if err != nil {
		return Profile{}, err
	}
	p := Profile{User: row.ref, JoinedAt: row.joined, Links: []Link{}, Sections: map[string]bool{},
		Viewer: ProfileViewer{IsSelf: v.Self, Following: v.Follower}}
	if !v.Self && !v.Anonymous && !v.Blocked {
		if p.Viewer.FollowsYou, err = follows(ctx, q, ownerID, viewerID); err != nil {
			return Profile{}, err
		}
	}
	for _, sec := range Sections() {
		p.Sections[sec.Key] = Visible(row.privacy.of(sec.Key), v)
	}
	if v.Blocked {
		p.Restricted = true
		p.Viewer.Following = false
		return p, nil
	}
	p.Bio, p.Links = row.bio, row.links
	p.Followers, p.Following = row.followers, row.following
	return p, nil
}

// target resolves {handle} (or "me") to a user id.
func (s *Service) target(r *http.Request) (uuid.UUID, error) {
	h := NormalizeHandle(chi.URLParam(r, "handle"))
	if h == "me" {
		return auth.MustFromContext(r.Context()).UserID, nil
	}
	id, _, err := resolveHandle(r.Context(), s.d.Pool, h)
	return id, err
}

func (s *Service) getProfile(w http.ResponseWriter, r *http.Request) error {
	owner, err := s.target(r)
	if err != nil {
		return err
	}
	p, err := BuildProfile(r.Context(), s.d.Pool, owner, auth.MustFromContext(r.Context()).UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, p)
	return nil
}

// ---- my profile ------------------------------------------------------------

func (s *Service) myProfile(ctx context.Context, q db.Querier, me uuid.UUID) (MyProfile, error) {
	row, err := loadProfile(ctx, q, me)
	if err != nil {
		return MyProfile{}, err
	}
	out := MyProfile{User: row.ref, Bio: row.bio, Links: row.links, HandleChangedAt: row.handleChanged}
	out.Limits.BioMaxChars, out.Limits.MaxLinks = s.bioMax, MaxLinks
	out.Limits.HandleCooldownHours, out.Limits.HandleRedirectHours = int(s.handleCooldown.Hours()), int(s.handleRedirect.Hours())
	if row.handleChanged != nil && s.handleCooldown > 0 {
		if next := row.handleChanged.Add(s.handleCooldown); time.Now().Before(next) {
			out.HandleChangeAt = &next
		}
	}
	for _, sec := range Sections() {
		out.Privacy = append(out.Privacy, PrivacyRow{Section: sec, Visibility: row.privacy.of(sec.Key)})
	}
	return out, nil
}

func (s *Service) getMe(w http.ResponseWriter, r *http.Request) error {
	p, err := s.myProfile(r.Context(), s.d.Pool, auth.MustFromContext(r.Context()).UserID)
	if err != nil {
		return err
	}
	httpx.OK(w, p)
	return nil
}

// ProfilePatch is PATCH /v1/social/me. Absent fields stay as they are.
type ProfilePatch struct {
	Bio     *string           `json:"bio"`
	Links   *[]Link           `json:"links"`
	Privacy map[string]string `json:"privacy"` // section key → visibility
}

// cleanLinks validates links: http(s) URLs with a host, no credentials,
// labels up to 40 characters.
func cleanLinks(in []Link) ([]Link, error) {
	if len(in) > MaxLinks {
		return nil, httpx.BadRequest("Add at most 5 links.")
	}
	out := make([]Link, 0, len(in))
	for _, l := range in {
		l.URL, l.Label = strings.TrimSpace(l.URL), strings.Join(strings.Fields(l.Label), " ")
		if l.URL == "" {
			continue
		}
		if !strings.Contains(l.URL, "://") {
			l.URL = "https://" + l.URL
		}
		u, err := url.Parse(l.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || !strings.Contains(u.Hostname(), ".") || len(l.URL) > 2048 {
			return nil, httpx.BadRequest("Links are web addresses, like https://example.com.")
		}
		if utf8.RuneCountInString(l.Label) > 40 {
			return nil, httpx.BadRequest("Link labels are at most 40 characters.")
		}
		out = append(out, l)
	}
	return out, nil
}

func (s *Service) patchMe(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	var body ProfilePatch
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	var bio *string
	if body.Bio != nil {
		b := strings.TrimSpace(*body.Bio)
		if n := utf8.RuneCountInString(b); n > s.bioMax {
			return httpx.BadRequest("Bios are at most " + grouped(s.bioMax) + " characters.")
		}
		bio = &b
	}
	var links []Link
	if body.Links != nil {
		var err error
		if links, err = cleanLinks(*body.Links); err != nil {
			return err
		}
	}
	// Text others will read goes through the content filter first.
	var text []string
	if bio != nil {
		text = append(text, *bio)
	}
	for _, l := range links {
		text = append(text, l.Label, l.URL)
	}
	if t := strings.TrimSpace(strings.Join(text, "\n")); t != "" {
		if err := moderation.Check(ctx, t); err != nil {
			return err
		}
	}
	var out MyProfile
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		row, err := loadProfile(ctx, tx, me)
		if err != nil {
			return err
		}
		priv := row.privacy
		if body.Privacy != nil {
			if priv, err = priv.apply(body.Privacy); err != nil {
				return err
			}
		}
		if bio == nil {
			bio = &row.bio
		}
		if body.Links == nil {
			links = row.links
		}
		lraw, _ := json.Marshal(links)
		praw, _ := json.Marshal(priv)
		if _, err := tx.Exec(ctx, `UPDATE social_profiles SET bio = $2, links = $3, privacy = $4, updated_at = now() WHERE user_id = $1`,
			me, *bio, lraw, praw); err != nil {
			return err
		}
		out, err = s.myProfile(ctx, tx, me)
		return err
	})
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// putHandle: PUT /v1/social/me/handle {handle}.
func (s *Service) putHandle(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	var body struct {
		Handle string `json:"handle"`
	}
	if err := httpx.Decode(r, &body); err != nil {
		return err
	}
	h := NormalizeHandle(body.Handle)
	if e := checkHandle(h, s.reservedFor(), staff(id)); e != nil {
		return e
	}
	if err := moderation.Check(ctx, strings.ReplaceAll(h, "_", " ")); err != nil {
		return err
	}
	var out MyProfile
	err := db.WithTx(ctx, s.d.Pool, func(tx pgx.Tx) error {
		if err := ensureProfiles(ctx, tx, []uuid.UUID{id.UserID}); err != nil {
			return err
		}
		if _, err := changeHandle(ctx, tx, id.UserID, h, s.handleCooldown, s.handleRedirect); err != nil {
			return err
		}
		var err error
		out, err = s.myProfile(ctx, tx, id.UserID)
		return err
	})
	if err != nil {
		return err
	}
	httpx.OK(w, out)
	return nil
}

// HandleCheck is GET /v1/social/handles/{handle}: whether the caller can
// take it, and why not.
type HandleCheck struct {
	Handle    string `json:"handle"`
	Available bool   `json:"available"`
	Code      string `json:"code,omitempty"`    // handle_invalid | handle_reserved | handle_taken | content_rejected
	Message   string `json:"message,omitempty"` // user-facing reason
}

func (s *Service) getHandleCheck(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := auth.MustFromContext(ctx)
	h := NormalizeHandle(chi.URLParam(r, "handle"))
	out := HandleCheck{Handle: h}
	fail := func(e *httpx.Error) error {
		out.Code, out.Message = e.Code, e.Message
		httpx.OK(w, out)
		return nil
	}
	if e := checkHandle(h, s.reservedFor(), staff(id)); e != nil {
		return fail(e)
	}
	if err := moderation.Check(ctx, strings.ReplaceAll(h, "_", " ")); err != nil {
		if e, ok := err.(*httpx.Error); ok {
			return fail(e)
		}
		return err
	}
	free, err := handleFree(ctx, s.d.Pool, id.UserID, h)
	if err != nil {
		return err
	}
	if !free {
		return fail(errHandleTaken)
	}
	out.Available = true
	httpx.OK(w, out)
	return nil
}

// ---- moderation target -----------------------------------------------------

// TargetProfile is the report kind for a profile's own content (handle, bio,
// links). Reporting the person is moderation's built-in "user".
const TargetProfile = "social.profile"

type profileTarget struct{ s *Service }

func (t profileTarget) Snapshot(ctx context.Context, id string) (moderation.Snapshot, error) {
	uid, err := uuid.Parse(id)
	if err != nil {
		return moderation.Snapshot{}, httpx.ErrNotFound
	}
	row, err := loadProfile(ctx, t.s.d.Pool, uid)
	if err != nil {
		return moderation.Snapshot{}, err
	}
	var b strings.Builder
	b.WriteString("@" + row.ref.Handle)
	if row.bio != "" {
		b.WriteString("\n\n" + row.bio)
	}
	for _, l := range row.links {
		b.WriteString("\n" + strings.TrimSpace(l.Label+" "+l.URL))
	}
	return moderation.Snapshot{AuthorID: &uid, Text: b.String(), MediaKeys: []string{}, Link: "/u/" + row.ref.Handle}, nil
}

// Remove clears the bio and links and swaps the handle for a generated one;
// the removed handle is held (redirecting nowhere) so nobody takes it at once.
func (t profileTarget) Remove(ctx context.Context, tx pgx.Tx, id string) error {
	uid, err := uuid.Parse(id)
	if err != nil {
		return httpx.ErrNotFound
	}
	var old, name, email string
	if err := tx.QueryRow(ctx, `SELECT p.handle, u.display_name, u.email FROM social_profiles p JOIN users u ON u.id = p.user_id
		WHERE p.user_id = $1 FOR UPDATE OF p`, uid).Scan(&old, &name, &email); err != nil {
		if isNoRows(err) {
			return nil
		}
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE social_profiles SET bio = '', links = '[]', updated_at = now() WHERE user_id = $1`, uid); err != nil {
		return err
	}
	// Their own old handles stop redirecting to them, and the removed one is
	// held so nobody takes it at once.
	if _, err := tx.Exec(ctx, `UPDATE social_handle_history SET user_id = NULL WHERE user_id = $1`, uid); err != nil {
		return err
	}
	if err := holdHandle(ctx, tx, old, t.s.handleRedirect); err != nil {
		return err
	}
	return regenerateHandle(ctx, tx, uid, name, email)
}

// holdHandle reserves h for nobody until d from now (a removed or deleted
// account's handle).
func holdHandle(ctx context.Context, q db.Querier, h string, d time.Duration) error {
	if h == "" || d <= 0 {
		return nil
	}
	_, err := q.Exec(ctx, `
		INSERT INTO social_handle_history (handle, user_id, expires_at) VALUES ($1, NULL, now() + $2::interval)
		ON CONFLICT (handle) DO UPDATE SET user_id = NULL, expires_at = EXCLUDED.expires_at`, h, interval(d))
	return err
}

// regenerateHandle gives uid a fresh generated handle without raising a
// conflict (safe inside someone else's transaction).
func regenerateHandle(ctx context.Context, q db.Querier, uid uuid.UUID, name, email string) error {
	cands := handleCandidates(name, email, uid)
	for _, h := range cands {
		tag, err := q.Exec(ctx, `
			UPDATE social_profiles SET handle = $2, updated_at = now() WHERE user_id = $1
			  AND NOT EXISTS (SELECT 1 FROM social_profiles WHERE handle = $2)
			  AND NOT EXISTS (SELECT 1 FROM social_handle_history WHERE handle = $2 AND expires_at > now())`, uid, h)
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			return nil
		}
	}
	return nil // every candidate taken (practically never): the handle stays
}
