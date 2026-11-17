package catalog

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/config"
	"scaffoldapp/backend/internal/safehttp"
)

// Source is where a sync reads the catalog. Register one with RegisterSource
// and select it with CATALOG_SOURCE.
type Source interface {
	// Name is the registered name ("http"), recorded on each sync run.
	Name() string
	// Fetch streams every item the source has now to emit, in any order, and
	// stops when emit returns an error (return that error). Only Key, Name,
	// Aliases, Category, Subtitle, ImageURL, Attrs, RefPrice and Currency are
	// read; the sync validates each item and skips (and counts) bad ones.
	// Report an item you can't even read with ReportSkipped. Any other error
	// aborts the sync and changes nothing. Respect ctx.
	Fetch(ctx context.Context, emit func(Item) error) error
}

type skipKey struct{}

// ReportSkipped records an item a source couldn't read (a malformed record):
// the sync run counts it as skipped and keeps the first few reasons. Call it
// with Fetch's ctx.
func ReportSkipped(ctx context.Context, err error) {
	if fn, ok := ctx.Value(skipKey{}).(func(error)); ok {
		fn(err)
	}
}

func withSkipReporter(ctx context.Context, fn func(error)) context.Context {
	return context.WithValue(ctx, skipKey{}, fn)
}

// skipTo is DecodeItems' bad callback for a source: ReportSkipped, then go on.
func skipTo(ctx context.Context) func(error) error {
	return func(err error) error { ReportSkipped(ctx, err); return nil }
}

// SourceInfo is optional: a short description of where a source reads
// (a URL's host, a file name), shown to staff. Never a secret.
type SourceInfo interface {
	Describe() string
}

var (
	registryMu sync.Mutex
	registry   = map[string]func(d *app.Deps) Source{}
)

// RegisterSource adds a source factory under name. Call it from an init()
// (stitching code) before the module is constructed. Only the selected
// source's factory runs (in New); it reads its own settings with config.* and
// calls config.Problem (returning nil) when they are missing or invalid.
func RegisterSource(name string, factory func(d *app.Deps) Source) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = factory
}

func init() {
	RegisterSource("http", newHTTPSource)
	RegisterSource("file", newFileSource)
}

// selectSource builds the source CATALOG_SOURCE names; unset, the http source
// when CATALOG_SOURCE_URL is set, else the file source when
// CATALOG_SOURCE_FILE is set, else none (no syncs).
func selectSource(d *app.Deps) Source {
	name := config.String("CATALOG_SOURCE", "")
	if name == "" {
		switch {
		case config.String("CATALOG_SOURCE_URL", "") != "":
			name = "http"
		case config.String("CATALOG_SOURCE_FILE", "") != "":
			name = "file"
		default:
			return nil
		}
	}
	registryMu.Lock()
	factory, ok := registry[name]
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	registryMu.Unlock()
	if !ok {
		sort.Strings(names)
		config.Problem("CATALOG_SOURCE=%q is not a registered catalog source (registered: %s)", name, strings.Join(names, ", "))
		return nil
	}
	return factory(d)
}

// ---- JSON and NDJSON ----

// wireItem is the JSON shape sources send: one object per item.
type wireItem struct {
	Key      string                     `json:"key"`
	Name     string                     `json:"name"`
	Aliases  []string                   `json:"aliases"`
	Category string                     `json:"category"`
	Subtitle string                     `json:"subtitle"`
	ImageURL string                     `json:"image_url"`
	Attrs    map[string]json.RawMessage `json:"attrs"`
	RefPrice *decimal.Decimal           `json:"ref_price"` // "12.50" or 12.5
	Currency string                     `json:"currency"`
}

// errBadItem marks an item the decoder could read but not use; the sync
// skips and counts it.
type errBadItem struct{ msg string }

func (e errBadItem) Error() string { return e.msg }

func (w wireItem) item() (Item, error) {
	it := Item{Key: w.Key, Name: w.Name, Aliases: w.Aliases, Category: w.Category, Subtitle: w.Subtitle,
		ImageURL: w.ImageURL, RefPrice: w.RefPrice, Currency: w.Currency}
	if len(w.Attrs) > 0 {
		it.Attrs = make(map[string]string, len(w.Attrs))
		for k, raw := range w.Attrs {
			var v any
			if err := json.Unmarshal(raw, &v); err != nil {
				return it, errBadItem{fmt.Sprintf("attrs.%s: %v", k, err)}
			}
			switch t := v.(type) {
			case nil:
			case string:
				it.Attrs[k] = t
			case bool:
				it.Attrs[k] = strconv.FormatBool(t)
			case float64:
				it.Attrs[k] = strings.TrimSpace(string(raw)) // as written: 26, 55.845
			default:
				return it, errBadItem{fmt.Sprintf("attrs.%s must be a string, number or boolean", k)}
			}
		}
	}
	return it, nil
}

// DecodeItems reads a JSON array of items or NDJSON (one item object per
// line), optionally gzipped, and calls emit for each. An item whose fields
// have the wrong JSON types goes to bad instead (skipped when bad is nil); a
// syntax error stops decoding. Either callback's error stops it too.
func DecodeItems(r io.Reader, emit func(Item) error, bad func(error) error) error {
	br := bufio.NewReaderSize(r, 64<<10)
	if magic, _ := br.Peek(2); len(magic) == 2 && magic[0] == 0x1f && magic[1] == 0x8b {
		zr, err := gzip.NewReader(br)
		if err != nil {
			return err
		}
		defer zr.Close()
		br = bufio.NewReaderSize(zr, 64<<10)
	}
	first, err := firstByte(br)
	if err == io.EOF {
		return nil // empty: no items
	}
	if err != nil {
		return err
	}
	dec := json.NewDecoder(br)
	one := func() error {
		var w wireItem
		err := dec.Decode(&w)
		var typeErr *json.UnmarshalTypeError
		switch {
		case errors.As(err, &typeErr):
			err = errBadItem{fmt.Sprintf("%s: wrong type (%s)", typeErr.Field, typeErr.Value)}
		case err != nil:
			return err
		}
		if err == nil {
			var it Item
			if it, err = w.item(); err == nil {
				return emit(it)
			}
		}
		if bad != nil {
			return bad(err)
		}
		return nil
	}
	switch first {
	case '[':
		if _, err := dec.Token(); err != nil {
			return err
		}
		for dec.More() {
			if err := one(); err != nil {
				return err
			}
		}
		_, err := dec.Token()
		return err
	case '{':
		for {
			err := one()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("expected a JSON array or NDJSON objects, got %q", first)
	}
}

func firstByte(br *bufio.Reader) (byte, error) {
	for {
		b, err := br.ReadByte()
		if err != nil {
			return 0, err
		}
		if b == 0xef { // a UTF-8 byte order mark
			if next, _ := br.Peek(2); len(next) == 2 && next[0] == 0xbb && next[1] == 0xbf {
				_, _ = br.Discard(2)
				continue
			}
		}
		if b != ' ' && b != '\t' && b != '\r' && b != '\n' {
			return b, br.UnreadByte()
		}
	}
}

// ---- built-in sources ----

// httpSource fetches CATALOG_SOURCE_URL (a JSON array or NDJSON, gzip
// allowed) through safehttp, with CATALOG_SOURCE_TOKEN as a bearer token.
type httpSource struct {
	url, token string
	maxMB      int
	client     *http.Client
}

func newHTTPSource(d *app.Deps) Source {
	dev := d.Cfg != nil && d.Cfg.IsDevelopment()
	raw := config.String("CATALOG_SOURCE_URL", "")
	maxMB := config.Int("CATALOG_SOURCE_MAX_MB", 256)
	timeout := config.Duration("CATALOG_SOURCE_TIMEOUT", 10*time.Minute)
	if raw == "" {
		config.Problem("CATALOG_SOURCE=http needs CATALOG_SOURCE_URL")
		return nil
	}
	if _, err := safehttp.CheckURL(raw, dev, dev); err != nil {
		config.Problem("CATALOG_SOURCE_URL can't be fetched: %v", err)
		return nil
	}
	if maxMB < 1 || maxMB > 4096 {
		config.Problem("CATALOG_SOURCE_MAX_MB must be 1 to 4096 (got %d)", maxMB)
		return nil
	}
	return &httpSource{
		url: raw, token: config.String("CATALOG_SOURCE_TOKEN", ""), maxMB: maxMB,
		client: safehttp.Client(safehttp.Options{Timeout: timeout, MaxBody: int64(maxMB) << 20, MaxRedirects: 3, AllowPrivate: dev, AllowHTTP: dev}),
	}
}

func (h *httpSource) Name() string { return "http" }

func (h *httpSource) Describe() string {
	if u, err := url.Parse(h.url); err == nil {
		return u.Host
	}
	return ""
}

func (h *httpSource) Fetch(ctx context.Context, emit func(Item) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json, application/x-ndjson")
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		if safehttp.IsBlocked(err) {
			return fmt.Errorf("the source address isn't reachable from this server")
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the source answered HTTP %d", resp.StatusCode)
	}
	err = DecodeItems(resp.Body, emit, skipTo(ctx))
	if errors.Is(err, safehttp.ErrTooLarge) {
		return fmt.Errorf("the source sent more than %d MB (CATALOG_SOURCE_MAX_MB)", h.maxMB)
	}
	return err
}

// fileSource reads CATALOG_SOURCE_FILE (a JSON array or NDJSON, gzip
// allowed): development, tests and offline machines.
type fileSource struct{ path string }

func newFileSource(_ *app.Deps) Source {
	p := config.String("CATALOG_SOURCE_FILE", "")
	if p == "" {
		config.Problem("CATALOG_SOURCE=file needs CATALOG_SOURCE_FILE")
		return nil
	}
	return &fileSource{path: p}
}

func (f *fileSource) Name() string     { return "file" }
func (f *fileSource) Describe() string { return filepath.Base(f.path) }

func (f *fileSource) Fetch(ctx context.Context, emit func(Item) error) error {
	fh, err := os.Open(f.path)
	if err != nil {
		return fmt.Errorf("can't open the source file: %w", err)
	}
	defer fh.Close()
	return DecodeItems(fh, func(it Item) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return emit(it)
	}, skipTo(ctx))
}

// ---- validation ----

// Limits on what a source may send. An item breaking one is skipped.
const (
	maxKeyLen     = 200
	maxNameLen    = 300
	maxAliases    = 50
	maxCatLen     = 100
	maxSubLen     = 300
	maxURLLen     = 2000
	maxAttrs      = 100
	maxAttrKeyLen = 100
	maxAttrValLen = 2000
)

var (
	keyRe      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:~-]*$`)
	currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)
)

// clean trims s and collapses its whitespace.
func clean(s string) string { return strings.Join(strings.Fields(s), " ") }

// normalize validates one item from a source and turns it into a row.
func normalize(it Item, allowHTTP bool) (rec, error) {
	r := rec{key: strings.TrimSpace(it.Key), name: clean(it.Name), category: clean(it.Category), subtitle: clean(it.Subtitle),
		imageURL: strings.TrimSpace(it.ImageURL)}
	switch {
	case r.key == "":
		return r, errors.New("key is required")
	case len(r.key) > maxKeyLen || !keyRe.MatchString(r.key):
		return r, fmt.Errorf("key %q must be 1 to %d letters, digits or . _ : ~ - (starting with a letter or digit)", trunc(r.key), maxKeyLen)
	case r.name == "" || NameKey(r.name) == "":
		return r, fmt.Errorf("%s: name is required", r.key)
	case utf8.RuneCountInString(r.name) > maxNameLen:
		return r, fmt.Errorf("%s: name is over %d characters", r.key, maxNameLen)
	case utf8.RuneCountInString(r.category) > maxCatLen:
		return r, fmt.Errorf("%s: category is over %d characters", r.key, maxCatLen)
	case utf8.RuneCountInString(r.subtitle) > maxSubLen:
		return r, fmt.Errorf("%s: subtitle is over %d characters", r.key, maxSubLen)
	case len(it.Aliases) > maxAliases:
		return r, fmt.Errorf("%s: more than %d aliases", r.key, maxAliases)
	case len(it.Attrs) > maxAttrs:
		return r, fmt.Errorf("%s: more than %d attributes", r.key, maxAttrs)
	}
	if r.imageURL != "" {
		u, err := url.Parse(r.imageURL)
		if err != nil || u.Host == "" || len(r.imageURL) > maxURLLen || !(u.Scheme == "https" || (allowHTTP && u.Scheme == "http")) {
			return r, fmt.Errorf("%s: image_url must be an https URL", r.key)
		}
	}
	r.aliases = mergeAliases(nil, r.name, it.Aliases)
	for _, a := range r.aliases {
		if utf8.RuneCountInString(a) > maxNameLen {
			return r, fmt.Errorf("%s: an alias is over %d characters", r.key, maxNameLen)
		}
	}
	if len(it.Attrs) > 0 {
		attrs := make(map[string]string, len(it.Attrs))
		for k, v := range it.Attrs {
			k = strings.TrimSpace(k)
			if k == "" || utf8.RuneCountInString(k) > maxAttrKeyLen || utf8.RuneCountInString(v) > maxAttrValLen {
				return r, fmt.Errorf("%s: attribute names are 1 to %d characters and values up to %d", r.key, maxAttrKeyLen, maxAttrValLen)
			}
			attrs[k] = strings.TrimSpace(v)
		}
		r.attrs = canonAttrs(attrs)
	}
	if it.RefPrice != nil {
		if it.RefPrice.IsNegative() {
			return r, fmt.Errorf("%s: ref_price can't be negative", r.key)
		}
		cur := strings.ToUpper(strings.TrimSpace(it.Currency))
		if !currencyRe.MatchString(cur) {
			return r, fmt.Errorf("%s: currency must be a 3-letter ISO 4217 code with ref_price", r.key)
		}
		r.price, r.currency = it.RefPrice.String(), cur
	}
	return r, nil
}

// mergeAliases adds the names in add to have (cleaned, in order), skipping
// any whose key matches name's or one already there.
func mergeAliases(have []string, name string, add []string) []string {
	keys := []string{NameKey(name)}
	out := make([]string, 0, len(have)+len(add))
	for _, a := range slices.Concat(have, add) {
		a = clean(a)
		k := NameKey(a)
		if k == "" || slices.Contains(keys, k) {
			continue
		}
		keys = append(keys, k)
		out = append(out, a)
	}
	return out
}

func trunc(s string) string {
	if len(s) > 40 {
		return s[:40] + "…"
	}
	return s
}
