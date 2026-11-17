package channels

// Pure parts of link previews: finding links in a message, normalising
// them, reading Open Graph / oEmbed metadata out of a page's head, and
// shrinking images to thumbnails. No I/O here (unfurl.go fetches).

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"html"
	"image"
	"image/color"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

const maxLinkPreviews = 3

var (
	urlRe        = regexp.MustCompile(`https?://[^\s<>"\x60]+`)
	fenceBlockRe = regexp.MustCompile("(?s)```.*?(```|$)")
	inlineCodeRe = regexp.MustCompile("`[^`\n]*`")
)

// extractLinks returns up to max distinct http(s) links in a message body,
// outside code, in order.
func extractLinks(body string, limit int) []string {
	body = fenceBlockRe.ReplaceAllString(body, " ")
	body = inlineCodeRe.ReplaceAllString(body, " ")
	out := []string{}
	seen := map[string]bool{}
	for _, raw := range urlRe.FindAllString(body, -1) {
		u := trimLinkTail(raw)
		if len(u) > 2000 || seen[u] {
			continue
		}
		p, err := url.Parse(u)
		if err != nil || p.Host == "" || p.User != nil {
			continue
		}
		seen[u] = true
		out = append(out, u)
		if len(out) == limit {
			break
		}
	}
	return out
}

// trimLinkTail drops trailing punctuation that belongs to the sentence, and a
// closing parenthesis or bracket without its opening one (markdown links).
func trimLinkTail(u string) string {
	for {
		if u == "" {
			return u
		}
		last := u[len(u)-1]
		switch last {
		case '.', ',', ';', ':', '!', '?', '\'', '*', '_', '~':
			u = u[:len(u)-1]
			continue
		case ')', ']':
			open := byte('(')
			if last == ']' {
				open = '['
			}
			if strings.Count(u, string(open)) < strings.Count(u, string(last)) {
				u = u[:len(u)-1]
				continue
			}
		}
		return u
	}
}

// normalizeLink is the cache key's form: lowercase scheme and host, no
// fragment, default ports dropped.
func normalizeLink(raw string) (string, string, bool) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", "", false
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Host = strings.TrimSuffix(strings.TrimSuffix(u.Host, ":443"), ":80")
	u.Fragment, u.RawFragment = "", ""
	if u.Path == "" {
		u.Path = "/"
	}
	n := u.String()
	sum := sha256.Sum256([]byte(n))
	return n, hex.EncodeToString(sum[:]), true
}

// pageMeta is what a page says about itself.
type pageMeta struct {
	Title, Description, SiteName string
	Image, Icon, OEmbed          string // absolute URLs
}

var (
	headEndRe = regexp.MustCompile(`(?i)</head\s*>|<body[\s>]`)
	metaTagRe = regexp.MustCompile(`(?is)<meta\s[^>]*>`)
	linkTagRe = regexp.MustCompile(`(?is)<link\s[^>]*>`)
	titleRe   = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title\s*>`)
	attrRe    = regexp.MustCompile(`(?s)([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'=<>\x60]+))`)
	spaceRe   = regexp.MustCompile(`\s+`)
)

func tagAttrs(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range attrRe.FindAllStringSubmatch(tag, -1) {
		v := m[2]
		if v == "" {
			v = m[3]
		}
		if v == "" {
			v = m[4]
		}
		out[strings.ToLower(m[1])] = html.UnescapeString(v)
	}
	return out
}

// cleanMeta collapses whitespace, strips control characters and caps length.
func cleanMeta(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\n' && r != '\t' || r == 0x7f {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
	if utf8.RuneCountInString(s) > max {
		s = strings.TrimSpace(string([]rune(s)[:max])) + "…"
	}
	return s
}

// parsePageMeta reads Open Graph, Twitter card and plain HTML metadata from a
// page's head. base resolves relative URLs.
func parsePageMeta(doc string, base *url.URL) pageMeta {
	if loc := headEndRe.FindStringIndex(doc); loc != nil {
		doc = doc[:loc[0]]
	}
	vals := map[string]string{}
	for _, tag := range metaTagRe.FindAllString(doc, -1) {
		a := tagAttrs(tag)
		key := strings.ToLower(a["property"])
		if key == "" {
			key = strings.ToLower(a["name"])
		}
		if key == "" || a["content"] == "" {
			continue
		}
		if _, ok := vals[key]; !ok {
			vals[key] = a["content"]
		}
	}
	pick := func(keys ...string) string {
		for _, k := range keys {
			if v := strings.TrimSpace(vals[k]); v != "" {
				return v
			}
		}
		return ""
	}
	m := pageMeta{
		Title:       pick("og:title", "twitter:title"),
		Description: pick("og:description", "twitter:description", "description"),
		SiteName:    pick("og:site_name", "application-name"),
		Image:       resolveRef(base, pick("og:image:secure_url", "og:image", "og:image:url", "twitter:image", "twitter:image:src")),
	}
	if m.Title == "" {
		if t := titleRe.FindStringSubmatch(doc); t != nil {
			m.Title = html.UnescapeString(t[1])
		}
	}
	iconRank := 0
	for _, tag := range linkTagRe.FindAllString(doc, -1) {
		a := tagAttrs(tag)
		rel := strings.ToLower(a["rel"])
		href := a["href"]
		if href == "" {
			continue
		}
		switch {
		case strings.Contains(rel, "alternate") && strings.EqualFold(a["type"], "application/json+oembed") && m.OEmbed == "":
			m.OEmbed = resolveRef(base, href)
		case strings.Contains(rel, "icon"):
			// Prefer a PNG icon (decodable), then apple-touch-icon, then anything.
			rank := 1
			if strings.Contains(rel, "apple-touch-icon") {
				rank = 2
			}
			if strings.EqualFold(a["type"], "image/png") || strings.HasSuffix(strings.ToLower(href), ".png") {
				rank = 3
			}
			if rank > iconRank {
				iconRank, m.Icon = rank, resolveRef(base, href)
			}
		}
	}
	if m.Icon == "" && base != nil {
		m.Icon = resolveRef(base, "/favicon.ico")
	}
	m.Title = cleanMeta(m.Title, 200)
	m.Description = cleanMeta(m.Description, 300)
	m.SiteName = cleanMeta(m.SiteName, 80)
	return m
}

// resolveRef makes ref absolute against base; only http(s) results survive.
func resolveRef(base *url.URL, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.HasPrefix(ref, "data:") {
		return ""
	}
	r, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	if base != nil {
		r = base.ResolveReference(r)
	}
	if (r.Scheme != "http" && r.Scheme != "https") || r.Host == "" || len(r.String()) > 2000 {
		return ""
	}
	return r.String()
}

// oembedMeta is the part of an oEmbed response previews use.
type oembedMeta struct {
	Title        string `json:"title"`
	AuthorName   string `json:"author_name"`
	ProviderName string `json:"provider_name"`
	ThumbnailURL string `json:"thumbnail_url"`
}

func parseOEmbed(body []byte, base *url.URL) (pageMeta, bool) {
	var o oembedMeta
	if err := json.Unmarshal(body, &o); err != nil {
		return pageMeta{}, false
	}
	m := pageMeta{Title: cleanMeta(o.Title, 200), SiteName: cleanMeta(o.ProviderName, 80), Image: resolveRef(base, o.ThumbnailURL)}
	if o.AuthorName != "" {
		m.Description = cleanMeta(o.AuthorName, 300)
	}
	return m, m.Title != ""
}

// thumbnail shrinks img to fit within size×size (never enlarging) by
// sampling at most 4×4 points per output pixel. With bg the result is
// composited onto it (JPEG has no alpha); without, alpha is kept (PNG icons).
func thumbnail(img image.Image, size int, bg color.Color) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	tw, th := w, h
	if w > size || h > size {
		if w >= h {
			tw, th = size, max(1, h*size/w)
		} else {
			tw, th = max(1, w*size/h), size
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, tw, th))
	var br, bgG, bb uint32
	if bg != nil {
		br, bgG, bb, _ = bg.RGBA()
	}
	for y := 0; y < th; y++ {
		y0, y1 := b.Min.Y+y*h/th, b.Min.Y+(y+1)*h/th
		y1 = max(y1, y0+1)
		for x := 0; x < tw; x++ {
			x0, x1 := b.Min.X+x*w/tw, b.Min.X+(x+1)*w/tw
			x1 = max(x1, x0+1)
			var r, g, bl, a, n uint64
			sx, sy := max(1, (x1-x0)/4), max(1, (y1-y0)/4)
			for yy := y0; yy < y1; yy += sy {
				for xx := x0; xx < x1; xx += sx {
					cr, cg, cb, ca := img.At(xx, yy).RGBA() // premultiplied
					if bg != nil {
						cr += br * (0xffff - ca) / 0xffff
						cg += bgG * (0xffff - ca) / 0xffff
						cb += bb * (0xffff - ca) / 0xffff
						ca = 0xffff
					}
					r, g, bl, a = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca)
					n++
				}
			}
			out.SetRGBA(x, y, color.RGBA{uint8(r / n >> 8), uint8(g / n >> 8), uint8(bl / n >> 8), uint8(a / n >> 8)})
		}
	}
	return out
}
