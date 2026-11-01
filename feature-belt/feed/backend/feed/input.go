package feed

import (
	"context"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/google/uuid"

	"scaffoldapp/backend/internal/httpx"
	"scaffoldapp/backend/internal/moderation"
	"scaffoldapp/backend/internal/storage"
)

// mediaInput is one attachment: a key returned by POST /v1/uploads (purpose
// feed_media for photos and video, feed_file for documents), optional pixel
// dimensions the client read before uploading (layout only), and for
// documents the file name to show.
type mediaInput struct {
	Key    string  `json:"key"`
	Width  *int    `json:"width"`
	Height *int    `json:"height"`
	Name   *string `json:"name"`
}

type mediaItem struct {
	key, kind, contentType string
	width, height          *int
	name                   *string
}

// kindByExt maps a key's extension to its kind and content type. The
// purpose prefix decides which kinds a key may have.
var kindByExt = map[string][2]string{
	".jpg":  {"image", "image/jpeg"},
	".png":  {"image", "image/png"},
	".heic": {"image", "image/heic"},
	".webp": {"image", "image/webp"},
	".gif":  {"image", "image/gif"},
	".mp4":  {"video", "video/mp4"},
	".mov":  {"video", "video/quicktime"},
	".pdf":  {"file", "application/pdf"},
	".txt":  {"file", "text/plain"},
	".csv":  {"file", "text/csv"},
	".zip":  {"file", "application/zip"},
	".docx": {"file", "application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
	".xlsx": {"file", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"},
	".pptx": {"file", "application/vnd.openxmlformats-officedocument.presentationml.presentation"},
}

// validateMedia checks count, ownership and type of client-sent keys.
// existing maps keys already on the post (edits) to their stored names;
// those stay valid whoever uploaded them.
func validateMedia(in []mediaInput, owner uuid.UUID, existing map[string]*string) ([]mediaItem, error) {
	if len(in) > maxMediaPerPost {
		return nil, httpx.BadRequest("A post can have up to 10 attachments.")
	}
	seen := map[string]bool{}
	out := make([]mediaItem, 0, len(in))
	for _, m := range in {
		key := strings.TrimSpace(m.Key)
		if seen[key] {
			continue
		}
		storedName, attached := existing[key]
		isFile := strings.HasPrefix(key, FilePurpose+"/")
		if !attached && !storage.KeyOwnedBy(key, MediaPurpose, owner) && !storage.KeyOwnedBy(key, FilePurpose, owner) {
			return nil, httpx.BadRequest("Upload the file first, then attach it.")
		}
		seen[key] = true
		kt, ok := kindByExt[strings.ToLower(path.Ext(key))]
		if !ok || (kt[0] == "file") != isFile {
			return nil, httpx.BadRequest("Attach photos, videos or documents only.")
		}
		for _, d := range []*int{m.Width, m.Height} {
			if d != nil && (*d <= 0 || *d > 20000) {
				return nil, httpx.BadRequest("Invalid media dimensions.")
			}
		}
		item := mediaItem{key: key, kind: kt[0], contentType: kt[1], width: m.Width, height: m.Height}
		if isFile {
			item.width, item.height = nil, nil
			name := fileName(m.Name, path.Ext(key))
			if m.Name == nil && storedName != nil {
				name = *storedName
			}
			item.name = &name
		}
		out = append(out, item)
	}
	return out, nil
}

// fileName cleans a document's display name: no directories or control
// characters, at most 120 characters, keeping the real extension.
func fileName(raw *string, ext string) string {
	name := ""
	if raw != nil {
		name = *raw
	}
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
	base := strings.TrimSuffix(name, path.Ext(name))
	if r := []rune(base); len(r) > 120 {
		base = string(r[:120])
	}
	if strings.TrimSpace(base) == "" {
		base = "Attachment"
	}
	return base + strings.ToLower(ext)
}

// normalizeLink validates an optional http(s) link. The server never fetches
// it (no previews), so there is no SSRF surface.
func normalizeLink(raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	v := strings.TrimSpace(*raw)
	if v == "" {
		return nil, nil
	}
	u, err := url.Parse(v)
	if err != nil || len(v) > maxLinkLen || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, httpx.BadRequest("Enter a full link starting with https://.")
	}
	return &v, nil
}

// checkText enforces the length limit and runs the moderation filter.
func checkText(ctx context.Context, text string, max int, what string) error {
	if utf8.RuneCountInString(text) > max {
		return httpx.BadRequest(what + " is over " + strconv.Itoa(max) + " characters. Shorten it and try again.")
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return moderation.Check(ctx, text)
}

// mentionInput is the write shape of an @mention: the user and the UTF-16
// offsets of their "@Name" token in the text.
type mentionInput struct {
	UserID uuid.UUID `json:"user_id"`
	Start  int       `json:"start"`
	Length int       `json:"length"`
}

type mentionRow struct {
	userID        uuid.UUID
	start, length int
}

// resolveMentions validates mentions against the text: each window must hold
// "@" followed by the user's current display name (or their id), so a client
// can't notify people the text doesn't visibly mention. Self-mentions,
// duplicates and users with a block either way are dropped silently.
func (s *Service) resolveMentions(ctx context.Context, author uuid.UUID, text string, in []mentionInput) ([]mentionRow, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if len(in) > maxMentions {
		return nil, httpx.BadRequest("Mention up to 20 people at a time.")
	}
	units := utf16.Encode([]rune(text))
	ids := make([]uuid.UUID, 0, len(in))
	for _, m := range in {
		if m.Start < 0 || m.Length < 2 || m.Start+m.Length > len(units) {
			return nil, httpx.BadRequest("Mentions don't match the text.")
		}
		ids = append(ids, m.UserID)
	}
	rows, err := s.d.Pool.Query(ctx, `
		SELECT id, display_name FROM users WHERE id = ANY($1) AND status IN ('active', 'suspended')`, ids)
	if err != nil {
		return nil, err
	}
	names := map[uuid.UUID]string{}
	for rows.Next() {
		var id uuid.UUID
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return nil, err
		}
		names[id] = name
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	blocked, err := moderation.BlockedIDs(ctx, s.d.Pool, author)
	if err != nil {
		return nil, err
	}
	skip := map[uuid.UUID]bool{author: true}
	for _, b := range blocked {
		skip[b] = true
	}

	out := make([]mentionRow, 0, len(in))
	for _, m := range in {
		name, ok := names[m.UserID]
		if !ok {
			return nil, httpx.BadRequest("Someone you mentioned isn't available.")
		}
		token := string(utf16.Decode(units[m.Start : m.Start+m.Length]))
		label, hasAt := strings.CutPrefix(token, "@")
		if !hasAt || !(sameName(label, name) || label == m.UserID.String()) {
			return nil, httpx.BadRequest("Mentions don't match the text.")
		}
		if skip[m.UserID] {
			continue
		}
		skip[m.UserID] = true
		out = append(out, mentionRow{userID: m.UserID, start: m.Start, length: m.Length})
	}
	return out, nil
}

func sameName(a, b string) bool {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	return a != "" && strings.EqualFold(a, b)
}

// preview shortens text for a notification body.
func preview(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	r := []rune(text)
	if len(r) <= previewLen {
		return text
	}
	return strings.TrimRightFunc(string(r[:previewLen]), unicode.IsSpace) + "…"
}

// searchQuery turns free text into a prefix tsquery ("ladd sat" →
// "ladd:* & sat:*"), keeping letters and digits only so user input can never
// break tsquery syntax. Empty means no search.
func searchQuery(q string) string {
	terms := strings.FieldsFunc(strings.ToLower(q), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	if len(terms) > 8 {
		terms = terms[:8]
	}
	for i, t := range terms {
		terms[i] = t + ":*"
	}
	return strings.Join(terms, " & ")
}

// likePrefix escapes LIKE wildcards in user input.
func likePrefix(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(q)
}

func actorName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "Someone"
	}
	return name
}
