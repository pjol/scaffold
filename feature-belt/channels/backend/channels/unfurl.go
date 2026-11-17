package channels

// Link previews. Outside links are fetched once through safehttp (the
// private-address guard runs on every dial and redirect), their Open Graph
// or oEmbed metadata cached, and their image and icon stored as small
// thumbnails, so readers load previews from this app's storage and never
// reveal themselves to the outside host. Links to the app's own resources
// become cards through unfurlers the stitching registers, resolved per
// viewer against their access (never cached, never broadcast).

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	_ "image/gif" // decoders for preview images
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"scaffoldapp/backend/internal/auth"
	"scaffoldapp/backend/internal/bg"
	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/safehttp"
	"scaffoldapp/backend/internal/storage"
)

const (
	// PurposeLinkImage holds preview thumbnails the server writes. MaxBytes 1
	// means no client upload fits: only the server puts objects here.
	PurposeLinkImage = "channel_link_image"

	previewFresh    = 24 * time.Hour      // linked again after this: fetched again
	previewRetry    = time.Hour           // a failed fetch is retried after this
	previewKeep     = 30 * 24 * time.Hour // previews no message links any more are dropped after this
	previewHTMLMax  = 768 << 10           // bytes of a page read to find its head
	previewImageMax = 5 << 20             // largest image fetched
	previewPixels   = 40_000_000          // largest decoded image (pixel bomb guard)
	thumbSize       = 600
	iconSize        = 64
	userAgent       = "Mozilla/5.0 (compatible; scaffoldapp-preview/1.0)"
)

// LinkPreview is a link in a message. External previews come from the
// cache; internal ones carry only the URL and clients ask GET /channels/card
// for the viewer's card.
type LinkPreview struct {
	URL         string  `json:"url"`
	Kind        string  `json:"kind"` // external | internal
	Title       string  `json:"title"`
	Description string  `json:"description"`
	SiteName    string  `json:"site_name"`
	ImageURL    *string `json:"image_url"`
	ImageWidth  *int    `json:"image_width"`
	ImageHeight *int    `json:"image_height"`
	IconURL     *string `json:"icon_url"`
}

// ---- the unfurler seam (the app's own resources) ---------------------------------------

// Card previews one of the app's resources as one viewer may see it.
type Card struct {
	Kind    string     `json:"kind"`    // "docs.page", "files.file", "crm.deal", "channels.message"…
	Title   string     `json:"title"`   // what the viewer would see as its name
	Context string     `json:"context"` // where it lives: "Docs · Engineering", "#general"
	Snippet string     `json:"snippet"` // a line of its content, may be ""
	Icon    string     `json:"icon"`    // a lucide icon name, like search results
	Link    string     `json:"link"`    // app path to open
	At      *time.Time `json:"at"`
}

// Unfurler turns a link to the app's own resource into a card for viewer,
// with the same access checks as the feature's routes. Return (nil, nil)
// when the viewer may not see it or it doesn't exist: the link then stays
// a plain link and nothing about the resource is revealed.
type Unfurler func(ctx context.Context, viewer *auth.Identity, u *url.URL) (*Card, error)

var (
	unfurlersMu sync.RWMutex
	unfurlers   = map[string]Unfurler{}
)

// RegisterUnfurler handles app paths starting with prefix ("/docs/",
// "/drive/"); the longest matching prefix wins. Links match as app paths or
// as absolute URLs on PUBLIC_WEB_URL.
func RegisterUnfurler(prefix string, fn Unfurler) {
	if !strings.HasPrefix(prefix, "/") || fn == nil {
		return
	}
	unfurlersMu.Lock()
	unfurlers[prefix] = fn
	unfurlersMu.Unlock()
}

func unfurlerFor(path string) Unfurler {
	unfurlersMu.RLock()
	defer unfurlersMu.RUnlock()
	best := ""
	for p := range unfurlers {
		if strings.HasPrefix(path, p) && len(p) > len(best) {
			best = p
		}
	}
	if best == "" {
		return nil
	}
	return unfurlers[best]
}

// internalURL reports whether raw points at this app: an absolute URL on
// PUBLIC_WEB_URL's origin, or an app path ("/docs/…", never "//host").
func (s *Service) internalURL(raw string) (*url.URL, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "/") && !strings.HasPrefix(raw, "//") {
		u, err := url.Parse(raw)
		return u, err == nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, false
	}
	web, err := url.Parse(s.d.Cfg.PublicWebURL)
	if err != nil || web.Host == "" {
		return nil, false
	}
	if strings.EqualFold(u.Host, web.Host) && strings.EqualFold(u.Scheme, web.Scheme) {
		return u, true
	}
	return nil, false
}

// cardFor resolves an internal link for viewer; nil when nobody handles it
// or the viewer may not see it.
func (s *Service) cardFor(ctx context.Context, viewer *auth.Identity, raw string) (*Card, error) {
	u, ok := s.internalURL(raw)
	if !ok {
		return nil, nil
	}
	fn := unfurlerFor(u.Path)
	if fn == nil {
		return nil, nil
	}
	card, err := fn(ctx, viewer, u)
	if err != nil || card == nil {
		return nil, err
	}
	if !strings.HasPrefix(card.Link, "/") || strings.HasPrefix(card.Link, "//") {
		card.Link = u.RequestURI()
	}
	card.Title, card.Context, card.Snippet = truncate(card.Title, 200), truncate(card.Context, 120), truncate(card.Snippet, 300)
	return card, nil
}

// GET /channels/card?url= — the viewer's card for a link to the app, 404 when
// there is none (unknown, or not theirs to see).
func (s *Service) getCard(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	caller := auth.MustFromContext(ctx)
	card, err := s.cardFor(ctx, caller, r.URL.Query().Get("url"))
	if err != nil {
		return err
	}
	if card == nil {
		return httpx.NotFound("No preview for that link.")
	}
	httpx.OK(w, card)
	return nil
}

// unfurlChannelLink is channels' own unfurler: /channels/<id>, with
// ?message=<id> (and ?thread=<id>) for a message link.
func (s *Service) unfurlChannelLink(ctx context.Context, viewer *auth.Identity, u *url.URL) (*Card, error) {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "channels" {
		return nil, nil
	}
	channelID, err := uuid.Parse(parts[1])
	if err != nil {
		return nil, nil
	}
	a, err := s.loadAccess(ctx, s.d.Pool, channelID, viewer.UserID)
	if err != nil {
		return nil, nil
	}
	if err := s.agentRequire(ctx, viewer, channelID); err != nil {
		return nil, nil
	}
	refs, err := s.channelRefs(ctx, viewer.UserID, []uuid.UUID{channelID})
	if err != nil {
		return nil, err
	}
	where := refs[channelID].Name
	if !a.direct() {
		where = "#" + where
	}
	msgID, err := uuid.Parse(u.Query().Get("message"))
	if err != nil {
		var topic string
		if err := s.d.Pool.QueryRow(ctx, `SELECT topic FROM channels WHERE id = $1`, channelID).Scan(&topic); err != nil {
			return nil, err
		}
		icon := "hash"
		if a.Kind != kindPublic {
			icon = "lock"
		}
		return &Card{Kind: "channels.channel", Title: where, Snippet: topic, Icon: icon, Link: "/channels/" + channelID.String()}, nil
	}
	m, err := s.loadMessage(ctx, s.d.Pool, msgID, viewer.UserID)
	if err != nil || m.ChannelID != channelID || m.Deleted || m.Kind == msgSystem {
		return nil, nil
	}
	if m.SenderID != nil {
		if blocked, err := moderation.IsBlocked(ctx, s.d.Pool, viewer.UserID, *m.SenderID); err != nil || blocked {
			return nil, err
		}
	}
	msg, err := s.message(ctx, s.d.Pool, msgID, viewer.UserID)
	if err != nil {
		return nil, nil
	}
	author := deref(msg.BotName)
	if msg.Sender != nil {
		author = msg.Sender.DisplayName
	}
	title := author
	if msg.Title != nil {
		title = *msg.Title
	}
	link := "/channels/" + channelID.String() + "?message=" + msgID.String()
	if msg.ThreadRootID != nil {
		link = "/channels/" + channelID.String() + "?thread=" + msg.ThreadRootID.String() + "&message=" + msgID.String()
	}
	at := msg.CreatedAt
	return &Card{Kind: "channels.message", Title: title, Context: where, Snippet: plainText(msg.Body, msg.Mentions), Icon: "message-square", Link: link, At: &at}, nil
}

// ---- saving a message's links ----------------------------------------------------------

// saveLinks records the links in a message body (replacing earlier ones).
func saveLinks(ctx context.Context, tx pgx.Tx, msgID uuid.UUID, body string) (int, error) {
	if _, err := tx.Exec(ctx, `DELETE FROM channel_message_links WHERE message_id = $1`, msgID); err != nil {
		return 0, err
	}
	n := 0
	for i, raw := range extractLinks(body, maxLinkPreviews) {
		norm, hash, ok := normalizeLink(raw)
		if !ok {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO channel_message_links (message_id, position, url, url_hash) VALUES ($1, $2, $3, $4)`,
			msgID, i, norm, hash); err != nil {
			return 0, err
		}
		n++
	}
	return n, nil
}

// kickUnfurl fetches the outside links of a message after its commit, then
// tells the channel the message changed.
func (s *Service) kickUnfurl(ctx context.Context, orgID, channelID, msgID uuid.UUID) {
	bg.Go(ctx, "channels.unfurl", func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		changed, err := s.unfurlMessage(ctx, orgID, msgID)
		if err != nil {
			slog.WarnContext(ctx, "channels: link previews", "message_id", msgID, "err", err)
		}
		if changed {
			if m, err := s.message(ctx, s.d.Pool, msgID, uuid.Nil); err == nil {
				s.publish(ctx, channelID, EventMessageUpdated, m)
			}
		}
		return nil
	})
}

// unfurlMessage fetches previews the message's outside links still need.
func (s *Service) unfurlMessage(ctx context.Context, orgID, msgID uuid.UUID) (bool, error) {
	ws, err := settingsOf(ctx, s.d.Pool, orgID)
	if err != nil || !ws.UnfurlLinks {
		return false, err
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT l.url, l.url_hash FROM channel_message_links l
		LEFT JOIN channel_link_previews p ON p.url_hash = l.url_hash
		WHERE l.message_id = $1
		  AND (p.url_hash IS NULL OR (p.status = 'error' AND p.fetched_at < $2) OR p.fetched_at < $3)
		ORDER BY l.position`, msgID, time.Now().Add(-previewRetry), time.Now().Add(-previewFresh))
	if err != nil {
		return false, err
	}
	type link struct{ url, hash string }
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (link, error) {
		var l link
		return l, r.Scan(&l.url, &l.hash)
	})
	if err != nil {
		return false, err
	}
	changed := false
	for _, l := range list {
		if _, internal := s.internalURL(l.url); internal {
			continue
		}
		s.unfurlSem <- struct{}{}
		p := s.fetchPreview(ctx, l.url)
		<-s.unfurlSem
		if ctx.Err() != nil {
			return changed, ctx.Err()
		}
		if _, err := s.d.Pool.Exec(ctx, `
			INSERT INTO channel_link_previews (url_hash, url, status, title, description, site_name, image_key, image_width, image_height, icon_key, fetched_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())
			ON CONFLICT (url_hash) DO UPDATE SET status = EXCLUDED.status, title = EXCLUDED.title, description = EXCLUDED.description,
			  site_name = EXCLUDED.site_name, image_key = EXCLUDED.image_key, image_width = EXCLUDED.image_width,
			  image_height = EXCLUDED.image_height, icon_key = EXCLUDED.icon_key, fetched_at = now()`,
			l.hash, l.url, p.status, p.meta.Title, p.meta.Description, p.meta.SiteName, p.imageKey, p.imageW, p.imageH, p.iconKey); err != nil {
			return changed, err
		}
		changed = changed || p.status == "ok"
	}
	return changed, nil
}

type fetched struct {
	status         string // ok | none | error
	meta           pageMeta
	imageKey       *string
	imageW, imageH *int
	iconKey        *string
}

// fetchPreview reads one outside page's metadata and stores its thumbnails.
func (s *Service) fetchPreview(ctx context.Context, raw string) fetched {
	base, err := url.Parse(raw)
	if err != nil {
		return fetched{status: "error"}
	}
	resp, err := s.get(ctx, s.pageClient, raw, "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5")
	if err != nil {
		if !safehttp.IsBlocked(err) {
			slog.DebugContext(ctx, "channels: fetch preview", "err", err)
		}
		return fetched{status: "error"}
	}
	defer resp.Body.Close()
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if resp.Request != nil && resp.Request.URL != nil {
		base = resp.Request.URL // after redirects
	}
	out := fetched{status: "none"}
	switch {
	case strings.HasPrefix(ct, "image/"):
		// A link straight to an image: the image is the preview.
		body, err := io.ReadAll(io.LimitReader(resp.Body, previewImageMax+1))
		if err != nil || len(body) > previewImageMax {
			return fetched{status: "error"}
		}
		if key, w, h, ok := s.storeThumb(ctx, body, thumbSize, true); ok {
			name := base.Path[strings.LastIndex(base.Path, "/")+1:]
			out = fetched{status: "ok", meta: pageMeta{Title: cleanMeta(name, 200), SiteName: cleanMeta(base.Hostname(), 80)}, imageKey: &key, imageW: &w, imageH: &h}
		}
		return out
	case ct != "text/html" && ct != "application/xhtml+xml":
		return out
	}
	doc, err := io.ReadAll(io.LimitReader(resp.Body, previewHTMLMax))
	if err != nil && len(doc) == 0 {
		return fetched{status: "error"}
	}
	meta := parsePageMeta(string(doc), base)
	if meta.OEmbed != "" && (meta.Title == "" || meta.Image == "") {
		if o, ok := s.fetchOEmbed(ctx, meta.OEmbed); ok {
			if meta.Title == "" {
				meta.Title = o.Title
			}
			if meta.Image == "" {
				meta.Image = o.Image
			}
			if meta.SiteName == "" {
				meta.SiteName = o.SiteName
			}
		}
	}
	if meta.SiteName == "" {
		meta.SiteName = cleanMeta(strings.TrimPrefix(base.Hostname(), "www."), 80)
	}
	if meta.Title == "" && meta.Description == "" {
		return out
	}
	out = fetched{status: "ok", meta: meta}
	if meta.Image != "" {
		if body, ok := s.fetchImage(ctx, meta.Image); ok {
			if key, w, h, ok := s.storeThumb(ctx, body, thumbSize, true); ok {
				out.imageKey, out.imageW, out.imageH = &key, &w, &h
			}
		}
	}
	if meta.Icon != "" {
		if body, ok := s.fetchImage(ctx, meta.Icon); ok {
			if key, _, _, ok := s.storeThumb(ctx, body, iconSize, false); ok {
				out.iconKey = &key
			}
		}
	}
	return out
}

func (s *Service) get(ctx context.Context, c *http.Client, raw, accept string) (*http.Response, error) {
	if _, err := safehttp.CheckURL(raw, s.devFetch, s.devFetch); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", accept)
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		resp.Body.Close()
		return nil, errors.New("status " + resp.Status)
	}
	return resp, nil
}

func (s *Service) fetchOEmbed(ctx context.Context, raw string) (pageMeta, bool) {
	resp, err := s.get(ctx, s.imageClient, raw, "application/json")
	if err != nil {
		return pageMeta{}, false
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return pageMeta{}, false
	}
	return parseOEmbed(body, resp.Request.URL)
}

func (s *Service) fetchImage(ctx context.Context, raw string) ([]byte, bool) {
	resp, err := s.get(ctx, s.imageClient, raw, "image/png,image/jpeg,image/gif,image/*;q=0.8")
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body) // the client caps the body (safehttp.ErrTooLarge)
	return body, err == nil
}

// storeThumb decodes an image (sniffed, never trusted by its header), refuses
// pixel bombs, shrinks it and stores it: JPEG over the surface colour for
// previews, PNG with alpha for icons.
func (s *Service) storeThumb(ctx context.Context, body []byte, size int, photo bool) (string, int, int, bool) {
	switch http.DetectContentType(body) {
	case "image/jpeg", "image/png", "image/gif":
	default:
		return "", 0, 0, false
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 12000 || cfg.Height > 12000 || cfg.Width*cfg.Height > previewPixels {
		return "", 0, 0, false
	}
	if photo && (cfg.Width < 32 || cfg.Height < 32) {
		return "", 0, 0, false // tracking pixels and spacers
	}
	img, _, err := image.Decode(bytes.NewReader(body))
	if err != nil {
		return "", 0, 0, false
	}
	var buf bytes.Buffer
	ct := "image/png"
	var thumb *image.RGBA
	if photo {
		ct = "image/jpeg"
		// Opaque white under transparent pixels: JPEG has no alpha.
		thumb = thumbnail(img, size, color.White)
		err = jpeg.Encode(&buf, thumb, &jpeg.Options{Quality: 80})
	} else {
		thumb = thumbnail(img, size, nil)
		err = png.Encode(&buf, thumb)
	}
	if err != nil {
		return "", 0, 0, false
	}
	key, err := storage.NewKey(PurposeLinkImage, uuid.Nil, ct)
	if err != nil {
		return "", 0, 0, false
	}
	if err := s.d.Storage.Put(ctx, key, ct, buf.Bytes()); err != nil {
		slog.WarnContext(ctx, "channels: store preview image", "err", err)
		return "", 0, 0, false
	}
	return key, thumb.Bounds().Dx(), thumb.Bounds().Dy(), true
}

// ---- reading previews -------------------------------------------------------------------

// hydrateLinks fills Links on messages whose author didn't remove them.
func (s *Service) hydrateLinks(ctx context.Context, q db.Querier, byID map[uuid.UUID]*Message, ids []uuid.UUID) error {
	rows, err := q.Query(ctx, `
		SELECT l.message_id, l.url, p.status, p.title, p.description, p.site_name, p.image_key, p.image_width, p.image_height, p.icon_key
		FROM channel_message_links l LEFT JOIN channel_link_previews p ON p.url_hash = l.url_hash
		WHERE l.message_id = ANY($1) ORDER BY l.message_id, l.position`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var msgID uuid.UUID
		var lp LinkPreview
		var status, title, desc, site, imageKey, iconKey *string
		if err := rows.Scan(&msgID, &lp.URL, &status, &title, &desc, &site, &imageKey, &lp.ImageWidth, &lp.ImageHeight, &iconKey); err != nil {
			return err
		}
		m := byID[msgID]
		if m == nil || m.LinksHidden {
			continue
		}
		if _, internal := s.internalURL(lp.URL); internal {
			m.Links = append(m.Links, LinkPreview{URL: lp.URL, Kind: "internal"})
			continue
		}
		if deref(status) != "ok" {
			continue
		}
		lp.Kind, lp.Title, lp.Description, lp.SiteName = "external", deref(title), deref(desc), deref(site)
		if imageKey != nil {
			if u, err := s.d.Storage.PresignGetWith(ctx, *imageKey, attachmentURLTTL, storage.GetOptions{ContentType: "image/jpeg", Disposition: "inline"}); err == nil {
				lp.ImageURL = &u
			}
		} else {
			lp.ImageWidth, lp.ImageHeight = nil, nil
		}
		if iconKey != nil {
			if u, err := s.d.Storage.PresignGetWith(ctx, *iconKey, attachmentURLTTL, storage.GetOptions{ContentType: "image/png", Disposition: "inline"}); err == nil {
				lp.IconURL = &u
			}
		}
		m.Links = append(m.Links, lp)
	}
	return rows.Err()
}

// DELETE /channels/messages/{id}/previews — the author removes the previews.
func (s *Service) hidePreviews(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	me := auth.MustFromContext(ctx).UserID
	id, err := httpx.URLParamUUID(r, "id")
	if err != nil {
		return err
	}
	m, err := s.loadMessage(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	if m.SenderID == nil || *m.SenderID != me {
		return httpx.Forbidden("Only the author can remove previews.")
	}
	if _, err := s.d.Pool.Exec(ctx, `UPDATE channel_messages SET links_hidden = true, updated_at = now() WHERE id = $1`, id); err != nil {
		return err
	}
	msg, err := s.message(ctx, s.d.Pool, id, me)
	if err != nil {
		return err
	}
	s.publishUpdated(ctx, m.access, msg, false)
	httpx.OK(w, msg)
	return nil
}

// prunePreviews drops cached previews no message links any more (their
// stored images are queued for deletion by trigger).
func (s *Service) prunePreviews(ctx context.Context) error {
	_, err := s.d.Pool.Exec(ctx, `
		DELETE FROM channel_link_previews p WHERE p.fetched_at < $1
		  AND NOT EXISTS (SELECT 1 FROM channel_message_links l WHERE l.url_hash = p.url_hash)`, time.Now().Add(-previewKeep))
	if err == nil {
		s.kickObjectCleanup(ctx)
	}
	return err
}
