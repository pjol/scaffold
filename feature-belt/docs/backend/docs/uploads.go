package docs

import (
	"bytes"
	"mime"
	"net/http"
	"path"
	"strings"
	"unicode/utf8"

	"scaffoldapp/backend/internal/httpx"
)

// Uploads: a page takes any file (purpose docs_file accepts any type, up to
// DOCS_FILE_MAX_MB). The stored type is settled here from what the client
// declared, the name and the first bytes, and files are served by type:
// images, video, audio, PDFs and plain text inline; SVG as an image (an
// <img> draws it, opening it downloads it, so its scripts never run);
// anything that could run script as a download of opaque bytes.

// uploadExtTypes types files whose client declared none (or octet-stream).
var uploadExtTypes = map[string]string{
	"jpg": "image/jpeg", "jpeg": "image/jpeg", "png": "image/png", "gif": "image/gif", "webp": "image/webp", "avif": "image/avif",
	"heic": "image/heic", "heif": "image/heic", "bmp": "image/bmp", "svg": "image/svg+xml", "ico": "image/x-icon",
	"mp4": "video/mp4", "m4v": "video/mp4", "webm": "video/webm", "mov": "video/quicktime", "ogv": "video/ogg",
	"mp3": "audio/mpeg", "wav": "audio/wav", "m4a": "audio/mp4", "aac": "audio/aac", "ogg": "audio/ogg", "oga": "audio/ogg", "opus": "audio/ogg", "flac": "audio/flac",
	"pdf": "application/pdf", "txt": "text/plain", "md": "text/markdown", "markdown": "text/markdown", "csv": "text/csv", "tsv": "text/tab-separated-values",
	"json": "application/json", "yaml": "application/yaml", "yml": "application/yaml", "xml": "application/xml", "html": "text/html", "htm": "text/html",
	"zip": "application/zip", "gz": "application/gzip", "tar": "application/x-tar", "7z": "application/x-7z-compressed",
	"doc": "application/msword", "xls": "application/vnd.ms-excel", "ppt": "application/vnd.ms-powerpoint", "rtf": "application/rtf",
	"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	"odt":  "application/vnd.oasis.opendocument.text", "ods": "application/vnd.oasis.opendocument.spreadsheet", "odp": "application/vnd.oasis.opendocument.presentation",
	"go": "text/x-go", "py": "text/x-python", "js": "text/javascript", "ts": "text/x-typescript", "tsx": "text/x-typescript", "jsx": "text/javascript",
	"rb": "text/x-ruby", "rs": "text/x-rust", "java": "text/x-java", "c": "text/x-c", "h": "text/x-c", "cpp": "text/x-c++", "cs": "text/x-csharp",
	"swift": "text/x-swift", "kt": "text/x-kotlin", "php": "text/x-php", "sql": "application/sql", "sh": "application/x-sh", "css": "text/css",
	"log": "text/plain", "ini": "text/plain", "toml": "application/toml",
}

// inlineUploadTypes are shown in the page (or a viewer) rather than downloaded.
var inlineUploadTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/avif": true, "image/bmp": true, "image/heic": true, "image/x-icon": true,
	"application/pdf": true, "video/mp4": true, "video/webm": true, "video/quicktime": true, "video/ogg": true,
	"audio/mpeg": true, "audio/mp4": true, "audio/aac": true, "audio/wav": true, "audio/x-wav": true, "audio/ogg": true, "audio/webm": true, "audio/flac": true,
	"text/plain": true,
}

var errUploadMismatch = httpx.NewError(http.StatusUnprocessableEntity, "content_type_mismatch",
	"This file's contents don't match its type. Check the file and upload it again.")

func baseMediaType(ct string) string {
	mt, _, err := mime.ParseMediaType(strings.TrimSpace(ct))
	if err != nil {
		return ""
	}
	return strings.ToLower(mt)
}

func uploadExt(name string) string {
	return strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
}

// textual types are text by name (served as plain text, never as a page).
func textual(mt string) bool {
	if strings.HasPrefix(mt, "text/") {
		return true
	}
	switch mt {
	case "application/json", "application/xml", "application/yaml", "application/x-yaml", "application/toml", "application/sql",
		"application/x-sh", "application/javascript", "image/svg+xml", "application/xhtml+xml":
		return true
	}
	return strings.HasSuffix(mt, "+json") || strings.HasSuffix(mt, "+xml")
}

// scriptable types can run script when a browser renders them as a document.
func scriptable(mt string) bool {
	return strings.Contains(mt, "html") || strings.Contains(mt, "javascript") || strings.Contains(mt, "ecmascript") ||
		strings.Contains(mt, "svg") || strings.HasSuffix(mt, "/xml") || strings.HasSuffix(mt, "+xml")
}

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

// classifyUpload settles an upload's stored type. It refuses a file that
// claims a type browsers render (image, video, audio, PDF, SVG) when its
// first bytes are something else, and text whose bytes are binary.
func classifyUpload(declared, name string, head []byte) (string, error) {
	mt := baseMediaType(declared)
	if mt == "" || mt == "application/octet-stream" || mt == "binary/octet-stream" {
		mt = uploadExtTypes[uploadExt(name)]
		if mt == "" {
			if len(head) > 0 && looksText(head) {
				mt = "text/plain"
			} else {
				mt = "application/octet-stream"
			}
		}
	}
	if len(head) == 0 {
		return mt, nil
	}
	sniffed := baseMediaType(http.DetectContentType(head))
	family, _, _ := strings.Cut(mt, "/")
	switch {
	case mt == "image/svg+xml":
		if !looksText(head) || !bytes.Contains(bytes.ToLower(head), []byte("<svg")) {
			return "", errUploadMismatch
		}
	case mt == "application/pdf":
		if sniffed != "application/pdf" {
			return "", errUploadMismatch
		}
	case family == "image":
		if !(strings.HasPrefix(sniffed, "image/") || sniffed == "application/octet-stream") || (looksText(head) && sniffed != "image/x-icon") {
			return "", errUploadMismatch
		}
	case family == "video" || family == "audio":
		if !(strings.HasPrefix(sniffed, "video/") || strings.HasPrefix(sniffed, "audio/") || sniffed == "application/ogg" || sniffed == "application/octet-stream") || looksText(head) {
			return "", errUploadMismatch
		}
	case textual(mt):
		if !looksText(head) {
			return "", errUploadMismatch
		}
	}
	return mt, nil
}

// servePolicy is the content type and disposition a stored file is served
// with.
func servePolicy(mt, name string) (contentType, disposition string) {
	switch {
	case mt == "image/svg+xml":
		contentType, disposition = mt, "attachment" // an <img> draws it; opening it downloads it
	case inlineUploadTypes[mt]:
		contentType, disposition = mt, "inline"
	case textual(mt) && !scriptable(mt):
		contentType, disposition = "text/plain; charset=utf-8", "inline"
	case scriptable(mt) || mt == "":
		contentType, disposition = "application/octet-stream", "attachment"
	default:
		contentType, disposition = mt, "attachment"
	}
	if contentType == "text/plain" {
		contentType += "; charset=utf-8"
	}
	return contentType, mime.FormatMediaType(disposition, map[string]string{"filename": name})
}
