package orgs

import (
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"
)

// The product's word for an organization in server copy: error messages,
// notification kinds, permission descriptions and agent tool descriptions,
// here and in every feature built on orgs. The client's word is
// configureOrgsNoun in the shared package; set both to the same word. API
// names, routes, JSON fields and kinds keep "org".

type noun struct{ one, many, oneTitle, manyTitle, a string }

var nounValue atomic.Pointer[noun]

// SetNoun names organizations in server copy: SetNoun("workspace",
// "workspaces"). Call it from an init function in the app's stitching
// package: modules word their permissions and notification kinds in New,
// so the word must be set before the first one is built. Messages are built
// when they are sent, never in package-level variables.
func SetNoun(one, many string) {
	one, many = strings.ToLower(strings.TrimSpace(one)), strings.ToLower(strings.TrimSpace(many))
	if one == "" || many == "" {
		panic("orgs.SetNoun needs both forms")
	}
	a := "a "
	if strings.ContainsRune("aeio", rune(one[0])) {
		a = "an "
	}
	nounValue.Store(&noun{one: one, many: many, oneTitle: capital(one), manyTitle: capital(many), a: a + one})
	refreshSentinels()
}

func capital(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}

func nounNow() *noun {
	if n := nounValue.Load(); n != nil {
		return n
	}
	return &noun{"organization", "organizations", "Organization", "Organizations", "an organization"}
}

// Noun is the word in lower case: "workspace".
func Noun() string { return nounNow().one }

// Nouns is the plural in lower case: "workspaces".
func Nouns() string { return nounNow().many }

// NounTitle is the word for titles and sentence starts: "Workspace".
func NounTitle() string { return nounNow().oneTitle }

// NounsTitle is the plural for titles and sentence starts: "Workspaces".
func NounsTitle() string { return nounNow().manyTitle }

// NounA is the word with its article: "a workspace", "an organization".
func NounA() string { return nounNow().a }
