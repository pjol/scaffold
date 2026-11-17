package app

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SearchQuery is a search box parsed into free text and operators, the one
// grammar global search and features share (the channels feature reads the
// same syntax in its own search):
//
//	from:@sam  from:me        who wrote or created it
//	in:#general  in:"Q3 plan"  where it lives (a channel, folder, page, record)
//	type:message  type:page    what it is (a kind like docs.page, its last part, or a feature)
//	is:unread  is:open         a state the feature knows
//	has:link  with:@sam  to:me features' own filters (channels)
//	before:2026-11-01  after:…  on:…  (also today, yesterday)
//	during:today|yesterday|week|month|year|2026|2026-11|november
//
// An operator with a value it can't read stays in the text, so a search for
// "ratio:3" still finds "ratio:3".
type SearchQuery struct {
	Text   string // the free text, operators removed, whitespace collapsed
	From   []string
	In     []string
	Types  []string
	Is     []string
	Has    []string
	With   []string
	To     []string
	After  *time.Time // at or after
	Before *time.Time // before
	// Ops lists the operators used, in order of first use ("from", "date", …).
	Ops []string
}

// Filtered reports whether any operator was used.
func (q SearchQuery) Filtered() bool { return len(q.Ops) > 0 }

// Uses reports whether operator op ("from", "in", "type", "is", "has",
// "with", "to", "date") was used.
func (q SearchQuery) Uses(op string) bool { return slices.Contains(q.Ops, op) }

// SearchOperatorKeys are the operator names ParseSearchQuery reads.
var SearchOperatorKeys = []string{"from", "in", "type", "is", "has", "with", "to", "before", "after", "on", "during"}

// SearchOperatorPattern matches one operator token: group 1 the space
// before it, 2 the key, 3 the value (quoted values keep their quotes).
const SearchOperatorPattern = `(?i)(^|\s)(from|in|type|is|has|with|to|before|after|on|during):("[^"]{1,100}"|\S+)`

var searchOpRE = regexp.MustCompile(SearchOperatorPattern)

var searchMonths = map[string]time.Month{
	"january": time.January, "jan": time.January, "february": time.February, "feb": time.February, "march": time.March, "mar": time.March,
	"april": time.April, "apr": time.April, "may": time.May, "june": time.June, "jun": time.June, "july": time.July, "jul": time.July,
	"august": time.August, "aug": time.August, "september": time.September, "sep": time.September, "sept": time.September,
	"october": time.October, "oct": time.October, "november": time.November, "nov": time.November, "december": time.December, "dec": time.December,
}

// SearchDay reads a day (2026-11-01, today, yesterday) as local midnight.
func SearchDay(v string, now time.Time, loc *time.Location) (time.Time, bool) {
	now = now.In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	switch strings.ToLower(v) {
	case "today":
		return today, true
	case "yesterday":
		return today.AddDate(0, 0, -1), true
	}
	t, err := time.ParseInLocation("2006-01-02", v, loc)
	return t, err == nil
}

// SearchSpan reads a during: value as [from, to).
func SearchSpan(v string, now time.Time, loc *time.Location) (time.Time, time.Time, bool) {
	now = now.In(loc)
	v = strings.ToLower(v)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	switch v {
	case "today", "yesterday":
		d, _ := SearchDay(v, now, loc)
		return d, d.AddDate(0, 0, 1), true
	case "week":
		start := today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7)) // Monday
		return start, start.AddDate(0, 0, 7), true
	case "month":
		start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		return start, start.AddDate(0, 1, 0), true
	case "year":
		start := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, loc)
		return start, start.AddDate(1, 0, 0), true
	}
	if t, err := time.ParseInLocation("2006-01", v, loc); err == nil {
		return t, t.AddDate(0, 1, 0), true
	}
	if t, err := time.ParseInLocation("2006", v, loc); err == nil {
		return t, t.AddDate(1, 0, 0), true
	}
	if t, err := time.ParseInLocation("2006-01-02", v, loc); err == nil {
		return t, t.AddDate(0, 0, 1), true
	}
	if m, ok := searchMonths[v]; ok {
		y := now.Year()
		if m > now.Month() {
			y-- // the most recent one
		}
		start := time.Date(y, m, 1, 0, 0, 0, 0, loc)
		return start, start.AddDate(0, 1, 0), true
	}
	return time.Time{}, time.Time{}, false
}

// ParseSearchQuery splits raw into text and operators; dates read in loc.
func ParseSearchQuery(raw string, now time.Time, loc *time.Location) SearchQuery {
	if loc == nil {
		loc = time.UTC
	}
	var q SearchQuery
	use := func(op string) {
		if !slices.Contains(q.Ops, op) {
			q.Ops = append(q.Ops, op)
		}
	}
	add := func(list *[]string, op, v string) {
		v = strings.ToLower(strings.TrimSpace(v))
		if v != "" && !slices.Contains(*list, v) && len(*list) < 10 {
			*list = append(*list, v)
		}
		use(op)
	}
	narrow := func(from, to *time.Time) {
		if from != nil && (q.After == nil || from.After(*q.After)) {
			q.After = from
		}
		if to != nil && (q.Before == nil || to.Before(*q.Before)) {
			q.Before = to
		}
		use("date")
	}
	text := searchOpRE.ReplaceAllStringFunc(raw, func(tok string) string {
		m := searchOpRE.FindStringSubmatch(tok)
		key, val := strings.ToLower(m[2]), strings.Trim(m[3], `"`)
		switch key {
		case "from":
			add(&q.From, "from", strings.TrimPrefix(val, "@"))
		case "in":
			add(&q.In, "in", strings.TrimPrefix(val, "#"))
		case "type":
			add(&q.Types, "type", val)
		case "is":
			add(&q.Is, "is", val)
		case "has":
			add(&q.Has, "has", val)
		case "with":
			add(&q.With, "with", strings.TrimPrefix(val, "@"))
		case "to":
			add(&q.To, "to", strings.TrimPrefix(val, "@"))
		case "before":
			d, ok := SearchDay(val, now, loc)
			if !ok {
				return tok
			}
			narrow(nil, &d)
		case "after":
			d, ok := SearchDay(val, now, loc)
			if !ok {
				return tok
			}
			next := d.AddDate(0, 0, 1)
			narrow(&next, nil)
		case "on":
			d, ok := SearchDay(val, now, loc)
			if !ok {
				return tok
			}
			next := d.AddDate(0, 0, 1)
			narrow(&d, &next)
		case "during":
			from, to, ok := SearchSpan(val, now, loc)
			if !ok {
				return tok
			}
			narrow(&from, &to)
		}
		return m[1]
	})
	q.Text = strings.Join(strings.Fields(text), " ")
	return q
}

// SearchFilter is the parsed operators of a global search with the people
// from: names resolved, handed to providers that apply operators themselves
// (SearchOperatorProvider) through SearchFilterFrom(ctx).
type SearchFilter struct {
	Query SearchQuery
	// FromIDs are the people from: matched (the caller for from:me); empty
	// with from: used means nobody matched.
	FromIDs []uuid.UUID
}

// SearchOperatorProvider is a SearchProvider that applies some operators
// itself, in SQL: it lists them ("from", "in", "type", "is", "has",
// "with", "to", "date"). Global search then calls Search with the text
// alone and the filter in the context (SearchFilterFrom), and applies what
// it can of the rest to the hits (type on Kind, dates on At, in on Context,
// from on AuthorID); a query with an operator neither can apply skips the
// provider. Providers that implement nothing get only plain-text queries.
type SearchOperatorProvider interface {
	SearchOperators() []string
}

type searchFilterKey struct{}

// WithSearchFilter hands the parsed operators to providers.
func WithSearchFilter(ctx context.Context, f SearchFilter) context.Context {
	return context.WithValue(ctx, searchFilterKey{}, f)
}

// SearchFilterFrom is the operator part of the current global search.
func SearchFilterFrom(ctx context.Context) (SearchFilter, bool) {
	f, ok := ctx.Value(searchFilterKey{}).(SearchFilter)
	return f, ok
}
