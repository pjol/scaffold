package files

import (
	"bytes"
	"fmt"
	"mime"
	"net/http"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"scaffoldapp/backend/internal/httpx"
)

// Content rules: what a file is, whether its bytes match the type the
// client declared, and how it may be served. Pure functions (tested).

// codeTypes maps source file extensions to the type stored for them. Browsers
// report many of these as "" or something wrong (.ts as video/mp2t), so a
// text file with one of these extensions is stored as this type.
var codeTypes = map[string]string{
	"go": "text/x-go", "py": "text/x-python", "rb": "text/x-ruby", "rs": "text/x-rust", "java": "text/x-java",
	"kt": "text/x-kotlin", "swift": "text/x-swift", "c": "text/x-c", "h": "text/x-c", "cpp": "text/x-c++", "cc": "text/x-c++",
	"hpp": "text/x-c++", "cs": "text/x-csharp", "php": "text/x-php", "js": "text/javascript", "mjs": "text/javascript",
	"cjs": "text/javascript", "jsx": "text/javascript", "ts": "text/typescript", "tsx": "text/typescript", "sh": "text/x-shellscript",
	"bash": "text/x-shellscript", "zsh": "text/x-shellscript", "sql": "text/x-sql", "json": "application/json",
	"yaml": "text/yaml", "yml": "text/yaml", "toml": "text/x-toml", "ini": "text/plain", "env": "text/plain",
	"xml": "text/xml", "html": "text/html", "htm": "text/html", "css": "text/css", "scss": "text/x-scss",
	"svg": "image/svg+xml", "vue": "text/plain", "svelte": "text/plain", "lua": "text/x-lua", "r": "text/x-r",
	"dart": "text/x-dart", "scala": "text/x-scala", "ex": "text/x-elixir", "exs": "text/x-elixir", "erl": "text/x-erlang",
	"hs": "text/x-haskell", "clj": "text/x-clojure", "pl": "text/x-perl", "proto": "text/plain", "graphql": "text/plain",
	"dockerfile": "text/plain", "makefile": "text/plain", "gitignore": "text/plain", "lock": "text/plain", "log": "text/plain",
	"diff": "text/x-diff", "patch": "text/x-diff", "tex": "text/x-tex",
}

// knownTypes fills in a missing or generic type from the extension.
var knownTypes = map[string]string{
	"txt": "text/plain", "md": "text/markdown", "markdown": "text/markdown", "csv": "text/csv", "tsv": "text/tab-separated-values",
	"pdf": "application/pdf", "png": "image/png", "jpg": "image/jpeg", "jpeg": "image/jpeg", "gif": "image/gif",
	"webp": "image/webp", "avif": "image/avif", "bmp": "image/bmp", "heic": "image/heic", "heif": "image/heic", "ico": "image/x-icon",
	"mp4": "video/mp4", "m4v": "video/mp4", "mov": "video/quicktime", "webm": "video/webm", "ogv": "video/ogg",
	"mp3": "audio/mpeg", "m4a": "audio/mp4", "aac": "audio/aac", "wav": "audio/wav", "ogg": "audio/ogg", "oga": "audio/ogg",
	"flac": "audio/flac", "opus": "audio/ogg", "zip": "application/zip", "gz": "application/gzip", "tar": "application/x-tar",
	"doc": "application/msword", "xls": "application/vnd.ms-excel", "ppt": "application/vnd.ms-powerpoint",
	"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	"odt":  "application/vnd.oasis.opendocument.text", "ods": "application/vnd.oasis.opendocument.spreadsheet",
	"odp": "application/vnd.oasis.opendocument.presentation", "rtf": "application/rtf", "key": "application/x-iwork-keynote-sffkey",
	"pages": "application/x-iwork-pages-sffpages", "numbers": "application/x-iwork-numbers-sffnumbers",
}

// inlineTypes are the only types ever served inline: browsers render them
// without running script.
var inlineTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/avif": true, "image/bmp": true,
	"application/pdf": true, "video/mp4": true, "video/webm": true, "video/quicktime": true, "video/ogg": true,
	"audio/mpeg": true, "audio/mp4": true, "audio/aac": true, "audio/wav": true, "audio/x-wav": true, "audio/ogg": true,
	"audio/webm": true, "audio/flac": true, "text/plain": true,
}

var officeTypes = map[string]bool{
	"application/msword": true, "application/vnd.ms-excel": true, "application/vnd.ms-powerpoint": true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   true,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         true,
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": true,
	"application/vnd.oasis.opendocument.text":                                   true, "application/vnd.oasis.opendocument.spreadsheet": true,
	"application/vnd.oasis.opendocument.presentation": true, "application/rtf": true,
	"application/x-iwork-keynote-sffkey": true, "application/x-iwork-pages-sffpages": true, "application/x-iwork-numbers-sffnumbers": true,
}

// extOf is the lower-case extension without the dot ("" for none). A name
// like "Makefile" or ".gitignore" counts as its own extension.
func extOf(name string) string {
	base := strings.ToLower(path.Base(strings.ReplaceAll(name, "\\", "/")))
	if i := strings.LastIndexByte(base, '.'); i >= 0 && i < len(base)-1 {
		return base[i+1:]
	}
	return base
}

// baseType strips parameters and lower-cases a content type.
func baseType(ct string) string {
	mt, _, err := mime.ParseMediaType(strings.TrimSpace(ct))
	if err != nil {
		return ""
	}
	return strings.ToLower(mt)
}

// isTextual reports whether a type is text by its name.
func isTextual(mt string) bool {
	if strings.HasPrefix(mt, "text/") {
		return true
	}
	switch mt {
	case "application/json", "application/xml", "application/javascript", "application/x-yaml", "application/yaml",
		"application/toml", "application/x-sh", "application/sql", "image/svg+xml", "application/xhtml+xml":
		return true
	}
	return strings.HasSuffix(mt, "+json") || strings.HasSuffix(mt, "+xml")
}

// looksText reports whether head is UTF-8 text: no NUL bytes and valid
// UTF-8, allowing a rune cut off at the end of the sample.
func looksText(head []byte) bool {
	if bytes.IndexByte(head, 0) >= 0 {
		return false
	}
	for len(head) > 0 {
		r, size := utf8.DecodeRune(head)
		if r == utf8.RuneError && size <= 1 {
			return len(head) < utf8.UTFMax && !utf8.FullRune(head)
		}
		head = head[size:]
	}
	return true
}

// errContentMismatch answers an upload whose bytes aren't what it claims.
var errContentMismatch = httpx.NewError(http.StatusUnprocessableEntity, "content_type_mismatch",
	"This file's contents don't match its type. Check the file and upload it again.")

// classify settles the stored type of an upload from what the client
// declared, the name and the first bytes. It refuses a file that claims a
// type browsers render inline (an image, PDF, video, audio) when its bytes
// are something else (HTML pretending to be a PNG). textLike reports UTF-8
// text without NUL bytes.
func classify(declared, name string, head []byte) (mt string, textLike bool, err error) {
	mt = baseType(declared)
	ext := extOf(name)
	textLike = looksText(head)
	if mt == "" || mt == "application/octet-stream" || mt == "binary/octet-stream" {
		if t, ok := knownTypes[ext]; ok {
			mt = t
		} else if t, ok := codeTypes[ext]; ok {
			mt = t
		} else if textLike && len(head) > 0 {
			mt = "text/plain"
		} else {
			mt = "application/octet-stream"
		}
	}
	if t, ok := codeTypes[ext]; ok && textLike && !isTextual(mt) && !strings.HasPrefix(mt, "image/") {
		mt = t // .ts reported as video/mp2t, .sql as application/x-sql …
	}
	sniffed := baseType(http.DetectContentType(head))
	family, _, _ := strings.Cut(mt, "/")
	switch {
	case mt == "application/pdf":
		if len(head) > 0 && sniffed != "application/pdf" {
			return "", false, errContentMismatch
		}
	case family == "image" && mt != "image/svg+xml":
		if len(head) > 0 && (textLike || !(strings.HasPrefix(sniffed, "image/") || sniffed == "application/octet-stream")) {
			return "", false, errContentMismatch
		}
	case family == "video" || family == "audio":
		ok := strings.HasPrefix(sniffed, "video/") || strings.HasPrefix(sniffed, "audio/") ||
			sniffed == "application/ogg" || sniffed == "application/octet-stream"
		if len(head) > 0 && (textLike || !ok) {
			return "", false, errContentMismatch
		}
	}
	return mt, textLike, nil
}

// previewKind is how clients show an item.
func previewKind(typ, mt, name string, textLike bool) string {
	if typ != TypeFile {
		return typ
	}
	ext := extOf(name)
	switch {
	case mt == "image/svg+xml" && textLike:
		return "svg"
	case inlineTypes[mt] && strings.HasPrefix(mt, "image/"):
		return "image"
	case mt == "application/pdf":
		return "pdf"
	case strings.HasPrefix(mt, "video/") && inlineTypes[mt]:
		return "video"
	case strings.HasPrefix(mt, "audio/") && inlineTypes[mt]:
		return "audio"
	case officeTypes[mt]:
		return "office"
	case !textLike:
		return "other"
	case mt == "text/markdown" || ext == "md" || ext == "markdown":
		return "markdown"
	case mt == "text/csv" || ext == "csv":
		return "csv"
	case mt == "text/plain" && codeTypes[ext] == "":
		return "text"
	case isTextual(mt) || codeTypes[ext] != "":
		return "code"
	}
	return "other"
}

// servePolicy decides the Content-Type and Content-Disposition of a
// download. Inline only for inlineTypes (text files preview as text/plain);
// everything else is an attachment, and types that could run script go out
// as application/octet-stream.
func servePolicy(mt, name string, textLike, wantInline bool) (contentType, disposition string) {
	switch {
	case wantInline && inlineTypes[mt]:
		contentType, disposition = mt, "inline"
	case wantInline && textLike && isTextual(mt) && !scriptable(mt):
		contentType, disposition = "text/plain; charset=utf-8", "inline"
	default:
		disposition = "attachment"
		if mt == "" || scriptable(mt) || isTextual(mt) && mt != "text/plain" && mt != "text/csv" {
			contentType = "application/octet-stream"
		} else {
			contentType = mt
		}
	}
	if strings.HasPrefix(contentType, "text/plain") && !strings.Contains(contentType, "charset") {
		contentType += "; charset=utf-8"
	}
	return contentType, mime.FormatMediaType(disposition, map[string]string{"filename": name})
}

// scriptable types can run script when a browser renders them.
func scriptable(mt string) bool {
	return strings.Contains(mt, "html") || strings.Contains(mt, "javascript") || strings.Contains(mt, "ecmascript") ||
		strings.Contains(mt, "svg") || strings.HasSuffix(mt, "/xml") || strings.HasSuffix(mt, "+xml")
}

// cleanName validates a drive name: 1–255 characters, no slashes or control
// characters, not "." or "..". Surrounding spaces are trimmed.
func cleanName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	switch {
	case name == "":
		return "", httpx.BadRequest("Enter a name.")
	case name == "." || name == "..":
		return "", httpx.BadRequest("Choose a different name.")
	case utf8.RuneCountInString(name) > 255 || !utf8.ValidString(name):
		return "", httpx.BadRequest("Use 255 characters or fewer.")
	case strings.ContainsAny(name, "/\\"):
		return "", httpx.BadRequest("Names can't contain / or \\.")
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", httpx.BadRequest("Names can't contain line breaks or control characters.")
		}
	}
	return name, nil
}

// numberedName is "report (2).pdf" for n = 2.
func numberedName(name string, n int, isFile bool) string {
	ext := ""
	if isFile {
		if i := strings.LastIndexByte(name, '.'); i > 0 && i >= len(name)-12 {
			name, ext = name[:i], name[i:]
		}
	}
	return fmt.Sprintf("%s (%d)%s", name, n, ext)
}

// ---- text and line edits ---------------------------------------------------------

// textDoc is a text file split into lines, remembering its line ending and
// whether it ended with one, so an edit rewrites only the lines it names.
type textDoc struct {
	lines    []string
	newline  string // "\n" or "\r\n"
	trailing bool   // the last line ended with a newline
}

func parseText(b []byte) textDoc {
	s := string(b)
	d := textDoc{newline: "\n"}
	if strings.Contains(s, "\r\n") {
		d.newline = "\r\n"
	}
	if s == "" {
		return d
	}
	d.trailing = strings.HasSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\n")
	for _, l := range strings.Split(s, "\n") {
		d.lines = append(d.lines, strings.TrimSuffix(l, "\r"))
	}
	return d
}

func (d textDoc) bytes() []byte {
	if len(d.lines) == 0 {
		return []byte{}
	}
	out := strings.Join(d.lines, d.newline)
	if d.trailing {
		out += d.newline
	}
	return []byte(out)
}

// splitReplacement turns replacement text into lines: "" is no lines, and one
// trailing newline is dropped ("a\nb\n" is two lines).
func splitReplacement(text string) []string {
	if text == "" {
		return nil
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimSuffix(text, "\n")
	return strings.Split(text, "\n")
}

// replaceLines replaces lines start..end (1-based, inclusive) with text.
// end = start-1 inserts before line start (start may be len+1 to append).
// It returns the new document and the exact old and new lines.
func (d textDoc) replaceLines(start, end int, text string) (textDoc, []string, []string, error) {
	n := len(d.lines)
	if start < 1 || start > n+1 {
		return d, nil, nil, httpx.BadRequest(fmt.Sprintf("start_line must be between 1 and %d.", n+1))
	}
	if end < start-1 || end > n {
		return d, nil, nil, httpx.BadRequest(fmt.Sprintf("end_line must be between %d and %d.", start-1, n))
	}
	before := append([]string(nil), d.lines[start-1:end]...)
	after := splitReplacement(text)
	lines := make([]string, 0, n-len(before)+len(after))
	lines = append(lines, d.lines[:start-1]...)
	lines = append(lines, after...)
	lines = append(lines, d.lines[end:]...)
	out := textDoc{lines: lines, newline: d.newline, trailing: d.trailing || n == 0}
	return out, before, after, nil
}

// numbered renders lines from..to with their numbers, for the read tool.
func (d textDoc) numbered(from, to int) string {
	var b strings.Builder
	for i := from; i <= to && i <= len(d.lines); i++ {
		fmt.Fprintf(&b, "%d\t%s\n", i, d.lines[i-1])
	}
	return b.String()
}

// truncateUTF8 cuts b to at most n bytes without splitting a rune.
func truncateUTF8(b []byte, n int) ([]byte, bool) {
	if len(b) <= n {
		return b, false
	}
	b = b[:n]
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return b, true
}

// likeEscape escapes LIKE wildcards in user input.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
