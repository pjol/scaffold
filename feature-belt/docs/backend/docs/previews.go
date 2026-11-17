package docs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/db"
	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/safehttp"
	"scaffoldapp/backend/internal/storage"
)

// Bookmark previews. The server fetches a link once through safehttp
// (public addresses only, https, capped size and time), reads its title,
// description, site name, favicon and preview image, and copies the two
// images into private storage (PreviewPurpose). Readers load those copies
// through short-lived links, so the third-party host never learns who reads
// the page; it sees one request from the server per week at most.

// PreviewPurpose is the private storage purpose for bookmark images.
const PreviewPurpose = "docs_preview"

const (
	previewTTL       = 7 * 24 * time.Hour
	previewFailedTTL = 24 * time.Hour
	previewHTMLBytes = 512 << 10
	previewImgBytes  = 1 << 20
	previewIconBytes = 256 << 10
)

// LinkPreview is a link's metadata (DocsLinkPreview in shared/types.ts).
// IconURL and ImageURL are short-lived links to the server's copies.
type LinkPreview struct {
	ID          uuid.UUID  `json:"id"`
	URL         string     `json:"url"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	SiteName    string     `json:"site_name"`
	IconURL     string     `json:"icon_url,omitempty"`
	ImageURL    string     `json:"image_url,omitempty"`
	Embed       *EmbedInfo `json:"embed,omitempty"`
}

// EmbedInfo says a link can also be shown as an embed.
type EmbedInfo struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
}

type previewRow struct {
	id                     uuid.UUID
	url, title, desc, site string
	iconKey, iconType      *string
	imageKey, imageType    *string
	status                 string
	fetchedAt              time.Time
}

func previewHash(u string) string {
	h := sha256.Sum256([]byte(u))
	return hex.EncodeToString(h[:])
}

// previewURL normalises a link for fetching (http(s), no fragment).
func (s *Service) previewURL(raw string) (*url.URL, error) {
	link := safeLink(raw)
	if link == "" || strings.HasPrefix(link, "mailto:") {
		return nil, httpx.BadRequest("Enter a link starting with http:// or https://.")
	}
	u, _ := url.Parse(link)
	u.Fragment, u.RawFragment = "", ""
	dev := s.d.Cfg != nil && s.d.Cfg.IsDevelopment()
	if u.Scheme == "http" && !dev {
		u.Scheme = "https"
	}
	if _, err := safehttp.CheckURL(u.String(), dev, dev); err != nil {
		return nil, httpx.BadRequest("That link can't be previewed.")
	}
	return u, nil
}

// linkPreview: POST /docs/link-preview {url} (signed in, 30/min): the link's
// preview, fetched now or from the week's cache.
func (s *Service) linkPreview(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	var req struct {
		URL string `json:"url"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		return err
	}
	u, err := s.previewURL(req.URL)
	if err != nil {
		return err
	}
	row, err := s.previewFor(ctx, u)
	if err != nil {
		return err
	}
	out := s.previewOut(ctx, row)
	if e, ok := embedFor(req.URL); ok {
		out.Embed = &EmbedInfo{Provider: e.Provider, Name: embedName(e.Provider)}
	}
	httpx.OK(w, out)
	return nil
}

// getLinkPreview: GET /docs/link-previews/{previewID} (signed in) and
// GET /docs/public/previews/{previewID} (public pages): a stored preview
// with fresh image links. Previews hold public web metadata only.
func (s *Service) getLinkPreview(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id, err := httpx.URLParamUUID(r, "previewID")
	if err != nil {
		return err
	}
	row, err := s.loadPreview(ctx, `id = $1`, id)
	if db.IsNoRows(err) {
		return httpx.NotFound("Preview not found.")
	}
	if err != nil {
		return err
	}
	w.Header().Set("Cache-Control", "private, max-age=600")
	httpx.OK(w, s.previewOut(ctx, row))
	return nil
}

func (s *Service) loadPreview(ctx context.Context, where string, arg any) (*previewRow, error) {
	var p previewRow
	err := s.d.Pool.QueryRow(ctx, `SELECT id, url, title, description, site_name, icon_key, icon_type, image_key, image_type, status, fetched_at
		FROM docs_link_previews WHERE `+where, arg).
		Scan(&p.id, &p.url, &p.title, &p.desc, &p.site, &p.iconKey, &p.iconType, &p.imageKey, &p.imageType, &p.status, &p.fetchedAt)
	return &p, err
}

func (s *Service) previewOut(ctx context.Context, p *previewRow) LinkPreview {
	out := LinkPreview{ID: p.id, URL: p.url, Title: p.title, Description: p.desc, SiteName: p.site}
	sign := func(key, ct *string) string {
		if key == nil || ct == nil || s.d.Storage == nil {
			return ""
		}
		u, err := s.d.Storage.PresignGetWith(ctx, *key, fileURLTTL, storage.GetOptions{ContentType: *ct, Disposition: "inline"})
		if err != nil {
			return ""
		}
		return u
	}
	out.IconURL, out.ImageURL = sign(p.iconKey, p.iconType), sign(p.imageKey, p.imageType)
	return out
}

// previewFor returns the cached preview of u or fetches it.
func (s *Service) previewFor(ctx context.Context, u *url.URL) (*previewRow, error) {
	link := u.String()
	hash := previewHash(link)
	cur, err := s.loadPreview(ctx, `url_hash = $1`, hash)
	if err == nil {
		ttl := previewTTL
		if cur.status == "failed" {
			ttl = previewFailedTTL
		}
		if time.Since(cur.fetchedAt) < ttl {
			return cur, nil
		}
	} else if !db.IsNoRows(err) {
		return nil, err
	}
	meta, ferr := s.fetchMeta(ctx, u)
	status := "ok"
	if ferr != nil {
		slog.InfoContext(ctx, "docs: link preview failed", "host", u.Host, "err", ferr)
		status = "failed"
		meta = pageMeta{title: u.Host}
	}
	var iconKey, iconType, imageKey, imageType *string
	if meta.image != "" {
		imageKey, imageType = s.copyImage(ctx, meta.image, previewImgBytes)
	}
	if status == "ok" {
		icon := meta.icon
		if icon == "" {
			icon = (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/favicon.ico"}).String()
		}
		iconKey, iconType = s.copyImage(ctx, icon, previewIconBytes)
	}
	var old []string
	if cur != nil && cur.id != uuid.Nil {
		for _, k := range []*string{cur.iconKey, cur.imageKey} {
			if k != nil {
				old = append(old, *k)
			}
		}
	}
	row := &previewRow{url: link, title: clip(meta.title, maxTitleChars), desc: clip(meta.desc, 500), site: clip(meta.site, 200),
		iconKey: iconKey, iconType: iconType, imageKey: imageKey, imageType: imageType, status: status, fetchedAt: time.Now()}
	if err := s.d.Pool.QueryRow(ctx, `
		INSERT INTO docs_link_previews (url_hash, url, title, description, site_name, icon_key, icon_type, image_key, image_type, status, fetched_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())
		ON CONFLICT (url_hash) DO UPDATE SET title = EXCLUDED.title, description = EXCLUDED.description, site_name = EXCLUDED.site_name,
			icon_key = EXCLUDED.icon_key, icon_type = EXCLUDED.icon_type, image_key = EXCLUDED.image_key, image_type = EXCLUDED.image_type,
			status = EXCLUDED.status, fetched_at = now()
		RETURNING id`, hash, link, row.title, row.desc, row.site, iconKey, iconType, imageKey, imageType, status).Scan(&row.id); err != nil {
		return nil, err
	}
	for _, k := range old {
		_ = s.d.Storage.Delete(context.WithoutCancel(ctx), k)
	}
	return row, nil
}

type pageMeta struct {
	title, desc, site, icon, image string
}

func (s *Service) client(maxRedirects int, timeout time.Duration) *http.Client {
	dev := s.d.Cfg != nil && s.d.Cfg.IsDevelopment()
	return safehttp.Client(safehttp.Options{Timeout: timeout, MaxBody: -1, MaxRedirects: maxRedirects, AllowPrivate: dev, AllowHTTP: dev})
}

func previewRequest(ctx context.Context, u string, accept string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Scaffold AppBot/1.0; link previews)")
	req.Header.Set("Accept", accept)
	return req, nil
}

// fetchMeta reads a page's metadata from the first 512 KB of its HTML.
func (s *Service) fetchMeta(ctx context.Context, u *url.URL) (pageMeta, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := previewRequest(ctx, u.String(), "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5")
	if err != nil {
		return pageMeta{}, err
	}
	res, err := s.client(5, 8*time.Second).Do(req)
	if err != nil {
		return pageMeta{}, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return pageMeta{}, errors.New(res.Status)
	}
	final := res.Request.URL
	ct := baseMediaType(res.Header.Get("Content-Type"))
	if ct != "" && ct != "text/html" && ct != "application/xhtml+xml" {
		name := final.Path[strings.LastIndex(final.Path, "/")+1:]
		if name == "" {
			name = final.Host
		}
		return pageMeta{title: name, site: final.Host}, nil
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, previewHTMLBytes))
	if err != nil {
		return pageMeta{}, err
	}
	return parseMeta(string(body), final), nil
}

var (
	titleRE   = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	metaTagRE = regexp.MustCompile(`(?is)<meta\s+[^>]*>`)
	linkTagRE = regexp.MustCompile(`(?is)<link\s+[^>]*>`)
	attrRE    = regexp.MustCompile(`(?is)([a-z:_-]+)\s*=\s*("([^"]*)"|'([^']*)'|([^\s"'>]+))`)
	spaceRE   = regexp.MustCompile(`\s+`)
)

func attrs(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range attrRE.FindAllStringSubmatch(tag, -1) {
		v := m[3] + m[4] + m[5]
		out[strings.ToLower(m[1])] = html.UnescapeString(v)
	}
	return out
}

// parseMeta reads Open Graph / Twitter / plain meta tags and icon links.
func parseMeta(doc string, base *url.URL) pageMeta {
	if i := strings.Index(strings.ToLower(doc), "</head>"); i > 0 {
		doc = doc[:i]
	}
	meta := map[string]string{}
	for _, tag := range metaTagRE.FindAllString(doc, 200) {
		a := attrs(tag)
		key := strings.ToLower(a["property"])
		if key == "" {
			key = strings.ToLower(a["name"])
		}
		if key != "" && a["content"] != "" {
			if _, seen := meta[key]; !seen {
				meta[key] = a["content"]
			}
		}
	}
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := strings.TrimSpace(spaceRE.ReplaceAllString(meta[k], " ")); v != "" {
				return v
			}
		}
		return ""
	}
	m := pageMeta{
		title: first("og:title", "twitter:title"),
		desc:  first("og:description", "twitter:description", "description"),
		site:  first("og:site_name", "application-name"),
		image: first("og:image:secure_url", "og:image", "twitter:image", "twitter:image:src"),
	}
	if m.title == "" {
		if t := titleRE.FindStringSubmatch(doc); t != nil {
			m.title = strings.TrimSpace(spaceRE.ReplaceAllString(html.UnescapeString(t[1]), " "))
		}
	}
	if m.site == "" {
		m.site = strings.TrimPrefix(base.Hostname(), "www.")
	}
	best := 0
	for _, tag := range linkTagRE.FindAllString(doc, 200) {
		a := attrs(tag)
		rel := strings.ToLower(a["rel"])
		score := 0
		switch {
		case strings.Contains(rel, "apple-touch-icon"):
			score = 3
		case rel == "icon" || rel == "shortcut icon":
			score = 2
			if strings.Contains(strings.ToLower(a["type"]), "svg") || strings.HasSuffix(strings.ToLower(a["href"]), ".svg") {
				score = 1 // drawn only when nothing else exists; copied only if not SVG
			}
		}
		if score > best && a["href"] != "" {
			best, m.icon = score, a["href"]
		}
	}
	resolve := func(ref string) string {
		if ref == "" {
			return ""
		}
		r, err := base.Parse(strings.TrimSpace(ref))
		if err != nil || (r.Scheme != "https" && r.Scheme != "http") {
			return ""
		}
		r.Fragment = ""
		return r.String()
	}
	m.icon, m.image = resolve(m.icon), resolve(m.image)
	return m
}

var previewImageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/x-icon": true, "image/vnd.microsoft.icon": true, "image/avif": true}

// copyImage downloads an image (never SVG) and stores it; nil when it
// can't (the bookmark then shows without it).
func (s *Service) copyImage(ctx context.Context, raw string, limit int64) (*string, *string) {
	if s.d.Storage == nil {
		return nil, nil
	}
	dev := s.d.Cfg != nil && s.d.Cfg.IsDevelopment()
	if _, err := safehttp.CheckURL(raw, dev, dev); err != nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := previewRequest(ctx, raw, "image/avif,image/webp,image/png,image/jpeg,image/gif,image/*;q=0.8")
	if err != nil {
		return nil, nil
	}
	res, err := s.client(3, 5*time.Second).Do(req)
	if err != nil {
		return nil, nil
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil || int64(len(body)) > limit || len(body) == 0 {
		return nil, nil
	}
	ct := baseMediaType(http.DetectContentType(body))
	if ct == "application/octet-stream" || ct == "text/plain" {
		// Go doesn't sniff AVIF or every icon: trust a declared raster type only.
		if d := baseMediaType(res.Header.Get("Content-Type")); previewImageTypes[d] && !looksText(body[:min(len(body), 512)]) {
			ct = d
		}
	}
	if !previewImageTypes[ct] {
		return nil, nil
	}
	key, err := storage.NewKey(PreviewPurpose, uuid.Nil, ct)
	if err != nil {
		return nil, nil
	}
	if err := s.d.Storage.Put(ctx, key, ct, body); err != nil {
		return nil, nil
	}
	return &key, &ct
}

// cleanPreviews drops previews no bookmark uses that are over 90 days old.
func (s *Service) cleanPreviews(ctx context.Context) error {
	rows, err := s.d.Pool.Query(ctx, `
		DELETE FROM docs_link_previews p WHERE p.fetched_at < now() - interval '90 days'
		AND NOT EXISTS (SELECT 1 FROM docs_blocks b WHERE b.type = 'bookmark' AND b.props->>'preview_id' = p.id::text)
		RETURNING icon_key, image_key`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var a, b *string
		if err := rows.Scan(&a, &b); err != nil {
			return err
		}
		for _, k := range []*string{a, b} {
			if k != nil {
				keys = append(keys, *k)
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, k := range keys {
		if s.d.Storage != nil {
			_ = s.d.Storage.Delete(ctx, k)
		}
	}
	return nil
}
