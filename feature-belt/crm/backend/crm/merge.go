package crm

import (
	"regexp"
	"sort"
	"strings"
)

// Merge fields in email templates and drafts: {{contact.first_name}}, with an
// optional fallback for empty values: {{contact.first_name | there}}. Values
// come only from records the sender can open; anything unknown or empty
// without a fallback renders as nothing and is reported as missing.

var mergeRe = regexp.MustCompile(`\{\{\s*([a-z_]+(?:\.[a-z_]+)?)\s*(?:\|\s*([^{}]*?)\s*)?\}\}`)

// MergeFields lists the fields templates may use, for the composer's picker.
var MergeFields = []MergeField{
	{"contact.first_name", "Contact first name"}, {"contact.last_name", "Contact last name"}, {"contact.name", "Contact full name"},
	{"contact.email", "Contact email"}, {"contact.job_title", "Contact job title"},
	{"company.name", "Company name"}, {"company.domain", "Company domain"},
	{"deal.name", "Deal name"}, {"deal.amount", "Deal amount"}, {"deal.close_date", "Deal close date"}, {"deal.stage", "Deal stage"},
	{"ticket.name", "Ticket subject"}, {"ticket.status", "Ticket status"},
	{"sender.name", "Your name"}, {"sender.first_name", "Your first name"}, {"sender.email", "Your email"},
	{"booking_link", "Your booking link"},
}

// MergeField is one field a template can use.
type MergeField struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

func knownMergeField(key string) bool {
	for _, f := range MergeFields {
		if f.Key == key {
			return true
		}
	}
	return false
}

// renderMerge fills text from vals and returns the keys that rendered empty
// without a fallback (unknown keys included), sorted and unique.
func renderMerge(text string, vals map[string]string) (string, []string) {
	missing := map[string]bool{}
	out := mergeRe.ReplaceAllStringFunc(text, func(m string) string {
		sub := mergeRe.FindStringSubmatch(m)
		key, fallback := sub[1], sub[2]
		v := strings.TrimSpace(vals[key])
		if v != "" && knownMergeField(key) {
			return v
		}
		if fallback != "" {
			return fallback
		}
		missing[key] = true
		return ""
	})
	list := make([]string, 0, len(missing))
	for k := range missing {
		list = append(list, k)
	}
	sort.Strings(list)
	return out, list
}

// unknownMergeFields lists keys in text that aren't merge fields (templates
// are checked when saved).
func unknownMergeFields(text string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range mergeRe.FindAllStringSubmatch(text, -1) {
		if !knownMergeField(m[1]) && !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

// firstName is the first word of a display name.
func firstName(name string) string {
	f, _, _ := strings.Cut(strings.TrimSpace(name), " ")
	return f
}
