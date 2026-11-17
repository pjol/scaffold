package files

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/ledongthuc/pdf"
)

// Text extraction for search and for agents reading documents: PDF through
// ledongthuc/pdf (BSD-3, pure Go), OOXML (docx, xlsx, pptx) and OpenDocument
// (odt, ods, odp) with the standard library (they are zips of XML). Pure
// functions over bytes; every read is capped so a zip bomb or a huge sheet
// can't take the server down.

const (
	maxExtractedText = 256 << 10 // bytes of text kept per file
	maxZipPart       = 32 << 20  // uncompressed bytes read from one part
	maxZipParts      = 400       // parts read from one file
	maxSheetCells    = 200_000
)

var errNoText = errors.New("files: no extractable text")

// extractFormat is how a file's text can be read ("" for none).
func extractFormat(name, mt string) string {
	switch ext := extOf(name); {
	case mt == "application/pdf" || ext == "pdf":
		return "pdf"
	case ext == "docx" || mt == "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return "docx"
	case ext == "xlsx" || mt == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":
		return "xlsx"
	case ext == "pptx" || mt == "application/vnd.openxmlformats-officedocument.presentationml.presentation":
		return "pptx"
	case ext == "odt" || ext == "ods" || ext == "odp" || strings.HasPrefix(mt, "application/vnd.oasis.opendocument."):
		return "odf"
	}
	return ""
}

// extractText returns the readable text of a document, at most
// maxExtractedText bytes, whitespace tidied. errNoText for formats it
// doesn't read or files without text.
func extractText(name, mt string, data []byte) (out string, err error) {
	defer func() {
		if r := recover(); r != nil { // the PDF reader panics on some malformed files
			out, err = "", fmt.Errorf("files: extract %s: %v", name, r)
		}
	}()
	var text string
	switch extractFormat(name, mt) {
	case "pdf":
		text, err = pdfText(data)
	case "docx":
		text, err = ooxmlText(data, "docx")
	case "xlsx":
		text, err = ooxmlText(data, "xlsx")
	case "pptx":
		text, err = ooxmlText(data, "pptx")
	case "odf":
		text, err = odfText(data)
	default:
		return "", errNoText
	}
	if err != nil {
		return "", err
	}
	text = tidyText(text)
	if text == "" {
		return "", errNoText
	}
	return text, nil
}

func pdfText(data []byte) (string, error) {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for i := 1; i <= r.NumPage() && b.Len() < maxExtractedText; i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		t, err := p.GetPlainText(nil)
		if err != nil {
			continue
		}
		b.WriteString(t)
		b.WriteString("\n\n")
	}
	return b.String(), nil
}

// tidyText collapses runs of spaces, keeps at most one blank line, and cuts
// at maxExtractedText on a rune boundary.
func tidyText(s string) string {
	s = strings.ToValidUTF8(s, "")
	var b strings.Builder
	blank, space := 0, false
	for _, line := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			blank++
			continue
		}
		if b.Len() > 0 {
			if blank > 0 {
				b.WriteString("\n\n")
			} else {
				b.WriteByte('\n')
			}
		}
		blank = 0
		space = false
		for _, r := range line {
			if r == ' ' || r == ' ' {
				if space {
					continue
				}
				space = true
				b.WriteByte(' ')
				continue
			}
			space = false
			b.WriteRune(r)
		}
		if b.Len() > maxExtractedText {
			break
		}
	}
	out := b.String()
	if len(out) > maxExtractedText {
		cut, _ := truncateUTF8([]byte(out), maxExtractedText)
		out = string(cut)
	}
	return strings.TrimSpace(out)
}

// zipParts opens an office zip and returns a reader for named parts.
type zipParts struct {
	files map[string]*zip.File
	reads int
}

func openZip(data []byte) (*zipParts, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	z := &zipParts{files: map[string]*zip.File{}}
	for _, f := range zr.File {
		z.files[strings.TrimPrefix(f.Name, "/")] = f
	}
	return z, nil
}

func (z *zipParts) read(name string) ([]byte, error) {
	f, ok := z.files[name]
	if !ok {
		return nil, fs404
	}
	if z.reads++; z.reads > maxZipParts {
		return nil, errors.New("files: too many parts")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, maxZipPart))
}

var fs404 = errors.New("files: part not found")

// xmlRels reads a part's relationships: id → target resolved against the part.
func (z *zipParts) rels(part string) map[string]string {
	dir, file := path.Split(part)
	b, err := z.read(dir + "_rels/" + file + ".rels")
	out := map[string]string{}
	if err != nil {
		return out
	}
	d := xml.NewDecoder(bytes.NewReader(b))
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "Relationship" {
			var id, target, mode string
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "Id":
					id = a.Value
				case "Target":
					target = a.Value
				case "TargetMode":
					mode = a.Value
				}
			}
			if mode != "External" && id != "" {
				if strings.HasPrefix(target, "/") {
					out[id] = strings.TrimPrefix(target, "/")
				} else {
					out[id] = path.Clean(dir + target)
				}
			}
		}
	}
	return out
}

func relID(se xml.StartElement) string {
	for _, a := range se.Attr {
		if a.Name.Local == "id" && strings.Contains(a.Name.Space, "relationships") {
			return a.Value
		}
	}
	return ""
}

// paraText streams an OOXML part, writing the text of <t> elements, tabs
// and breaks, and a newline after each paragraph (<p>) or table row (<tr>).
func paraText(b []byte, out *strings.Builder) {
	d := xml.NewDecoder(bytes.NewReader(b))
	inT := false
	inCell := 0
	gap := false // a paragraph ended inside a table cell: a space before more text
	for out.Len() < maxExtractedText*2 {
		tok, err := d.Token()
		if err != nil {
			return
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "t":
				inT = true
			case "tab":
				out.WriteByte('\t')
			case "br", "cr":
				out.WriteByte('\n')
			case "tc":
				inCell++
				gap = false
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inT = false
			case "p":
				if inCell > 0 {
					gap = true
				} else {
					out.WriteByte('\n')
				}
			case "tc":
				inCell--
				gap = false
				out.WriteByte('\t')
			case "tr":
				out.WriteByte('\n')
			}
		case xml.CharData:
			if inT {
				if gap {
					out.WriteByte(' ')
					gap = false
				}
				out.Write(t)
			}
		}
	}
}

func ooxmlText(data []byte, format string) (string, error) {
	z, err := openZip(data)
	if err != nil {
		return "", err
	}
	var out strings.Builder
	switch format {
	case "docx":
		b, err := z.read("word/document.xml")
		if err != nil {
			return "", err
		}
		paraText(b, &out)
		for _, extra := range []string{"word/footnotes.xml", "word/endnotes.xml"} {
			if b, err := z.read(extra); err == nil {
				paraText(b, &out)
			}
		}
	case "pptx":
		pres := "ppt/presentation.xml"
		b, err := z.read(pres)
		if err != nil {
			return "", err
		}
		rels := z.rels(pres)
		d := xml.NewDecoder(bytes.NewReader(b))
		n := 0
		for {
			tok, err := d.Token()
			if err != nil {
				break
			}
			if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "sldId" {
				slide, ok := rels[relID(se)]
				if !ok {
					continue
				}
				n++
				sb, err := z.read(slide)
				if err != nil {
					continue
				}
				fmt.Fprintf(&out, "Slide %d\n", n)
				paraText(sb, &out)
				out.WriteByte('\n')
			}
		}
	case "xlsx":
		return xlsxText(z)
	}
	return out.String(), nil
}

func xlsxText(z *zipParts) (string, error) {
	wb := "xl/workbook.xml"
	b, err := z.read(wb)
	if err != nil {
		return "", err
	}
	var strs []string
	if sb, err := z.read("xl/sharedStrings.xml"); err == nil {
		d := xml.NewDecoder(bytes.NewReader(sb))
		var cur strings.Builder
		inSI, inT := false, false
		for {
			tok, err := d.Token()
			if err != nil {
				break
			}
			switch t := tok.(type) {
			case xml.StartElement:
				if t.Name.Local == "si" {
					inSI = true
					cur.Reset()
				} else if t.Name.Local == "t" && inSI {
					inT = true
				}
			case xml.EndElement:
				if t.Name.Local == "si" {
					inSI = false
					strs = append(strs, cur.String())
				} else if t.Name.Local == "t" {
					inT = false
				}
			case xml.CharData:
				if inT {
					cur.Write(t)
				}
			}
		}
	}
	rels := z.rels(wb)
	var out strings.Builder
	cells := 0
	d := xml.NewDecoder(bytes.NewReader(b))
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "sheet" {
			continue
		}
		name := ""
		for _, a := range se.Attr {
			if a.Name.Local == "name" {
				name = a.Value
			}
		}
		part, ok := rels[relID(se)]
		if !ok {
			continue
		}
		sheet, err := z.read(part)
		if err != nil {
			continue
		}
		out.WriteString(name + "\n")
		sd := xml.NewDecoder(bytes.NewReader(sheet))
		var typ, val string
		inV, inIS := false, false
		row := []string{}
		for cells < maxSheetCells {
			tok, err := sd.Token()
			if err != nil {
				break
			}
			switch t := tok.(type) {
			case xml.StartElement:
				switch t.Name.Local {
				case "c":
					typ, val = "", ""
					for _, a := range t.Attr {
						if a.Name.Local == "t" {
							typ = a.Value
						}
					}
				case "v":
					inV = true
				case "is":
					inIS = true
				case "t":
					if inIS {
						inV = true
					}
				}
			case xml.EndElement:
				switch t.Name.Local {
				case "v", "t":
					inV = false
				case "is":
					inIS = false
				case "c":
					cells++
					if typ == "s" {
						if i, err := strconv.Atoi(val); err == nil && i >= 0 && i < len(strs) {
							val = strs[i]
						}
					}
					if val != "" {
						row = append(row, val)
					}
				case "row":
					if len(row) > 0 {
						out.WriteString(strings.Join(row, "\t") + "\n")
					}
					row = row[:0]
				}
			case xml.CharData:
				if inV {
					val += string(t)
				}
			}
		}
		out.WriteByte('\n')
	}
	return out.String(), nil
}

// odfText reads OpenDocument content.xml: every text node in the body, a
// newline after paragraphs, headings and rows, tabs and repeated spaces.
func odfText(data []byte) (string, error) {
	z, err := openZip(data)
	if err != nil {
		return "", err
	}
	b, err := z.read("content.xml")
	if err != nil {
		return "", err
	}
	var out strings.Builder
	d := xml.NewDecoder(bytes.NewReader(b))
	inBody, skip := false, 0
	for out.Len() < maxExtractedText*2 {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "body":
				inBody = true
			case "tab":
				out.WriteByte('\t')
			case "line-break":
				out.WriteByte('\n')
			case "s":
				n := 1
				for _, a := range t.Attr {
					if a.Name.Local == "c" {
						if v, err := strconv.Atoi(a.Value); err == nil && v > 0 && v < 100 {
							n = v
						}
					}
				}
				out.WriteString(strings.Repeat(" ", n))
			case "annotation", "notes", "tracked-changes":
				skip++
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "p", "h", "table-row":
				out.WriteByte('\n')
			case "table-cell":
				out.WriteByte('\t')
			case "annotation", "notes", "tracked-changes":
				skip--
			}
		case xml.CharData:
			if inBody && skip == 0 {
				out.Write(t)
			}
		}
	}
	return out.String(), nil
}

// embeddedThumbnail is the preview picture an office file carries
// (docProps/thumbnail.* in OOXML saved with one, Thumbnails/thumbnail.png in
// every OpenDocument file), or nil.
func embeddedThumbnail(data []byte) []byte {
	z, err := openZip(data)
	if err != nil {
		return nil
	}
	names := make([]string, 0, 4)
	for n := range z.files {
		l := strings.ToLower(n)
		if l == "thumbnails/thumbnail.png" || strings.HasPrefix(l, "docprops/thumbnail.") && (strings.HasSuffix(l, ".jpeg") || strings.HasSuffix(l, ".jpg") || strings.HasSuffix(l, ".png")) {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		if b, err := z.read(n); err == nil && len(b) > 0 {
			return b
		}
	}
	return nil
}
