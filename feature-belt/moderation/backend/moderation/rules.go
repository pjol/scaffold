package moderation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
)

// RulesVersion is recorded on every decision so thresholds can be tuned
// from moderation_decisions.
const RulesVersion = "rules-v1"

// Decision actions, in increasing severity. The final action is the more
// severe of the rules' and the decision model's, so the model can only
// escalate.
const (
	DecisionAllow  = "allow"
	DecisionLabel  = "label"
	DecisionHold   = "hold"
	DecisionRemove = "remove"
)

var decisionRank = map[string]int{DecisionAllow: 0, DecisionLabel: 1, DecisionHold: 2, DecisionRemove: 3}

// DecisionRank orders decision actions (unknown = allow).
func DecisionRank(action string) int { return decisionRank[action] }

// MaxDecision is the more severe of a and b.
func MaxDecision(a, b string) string {
	if DecisionRank(b) > DecisionRank(a) {
		return b
	}
	if a == "" {
		return DecisionAllow
	}
	return a
}

// Categories of decisions, cases and strikes.
const (
	CatSpam         = "spam"
	CatScam         = "scam"
	CatHarassment   = "harassment"
	CatManipulation = "manipulation"
	CatOffTopic     = "off_topic"
	CatInjection    = "injection"
	CatOther        = "other"
)

// LabelOffTopic is the label a decision puts on an item that looks
// off-topic for its place (the forum ranks it lower).
const LabelOffTopic = "off_topic"

// Hit is one rule (or model answer) that fired.
type Hit struct {
	Rule     string `json:"rule"`
	Action   string `json:"action"`
	Category string `json:"category,omitempty"`
	Detail   string `json:"detail,omitempty"` // for moderators: the matched domain, count, probability
	Reason   string `json:"reason,omitempty"` // user-safe
	flag     string // the rule in words for the model's rule_flags
}

// Verdict is the rules' (or the model's) decision on one item.
type Verdict struct {
	Action     string
	Categories []string
	Labels     []string
	Hits       []Hit
	Rejected   bool // blocked term: refused, not stored

	Links    []Link // passed on to the decision model
	Mentions int
}

func (v *Verdict) add(h Hit) {
	v.Hits = append(v.Hits, h)
	v.Action = MaxDecision(v.Action, h.Action)
	if h.Category != "" && !slices.Contains(v.Categories, h.Category) {
		v.Categories = append(v.Categories, h.Category)
	}
}

func (v *Verdict) addLabel(l string) {
	if !slices.Contains(v.Labels, l) {
		v.Labels = append(v.Labels, l)
	}
}

// flags are the hits in words for the model's rule_flags field.
func (v Verdict) flags() []string {
	var out []string
	for _, h := range v.Hits {
		if h.flag != "" && !slices.Contains(out, h.flag) {
			out = append(out, h.flag)
		}
	}
	return out
}

// Facts are what moderation knows about the author, from its own data and
// what the UGC feature passed in the Item.
type Facts struct {
	AccountAge       time.Duration
	Karma            *int // the feature's standing (forum karma); nil when it has none
	Staff            bool // support or admin
	AuthorRepeats24h int  // earlier items by the author with the same text in the last 24 h
	OtherAccounts24h int  // other accounts that posted the same text in the last 24 h
	Removals90d      int  // the author's items held or removed in 90 days
	Strikes90d       int
}

// RuleInput is one item for the rules.
type RuleInput struct {
	Title, Body, URL string
	Facts            Facts
}

// Rules is the deterministic first stage (rules-v1).
type Rules struct {
	terms     *filter // MODERATION_BLOCKED_TERMS
	denylist  []string
	brands    Brands
	staffRe   *regexp.Regexp
	maxLinks  int
	minRepeat int
}

// NewRules builds the rules: blocked terms, a link denylist, the app's own
// domains (look-alikes of them are unsafe) and the names people use for the
// app (claims to be its staff are held).
func NewRules(blockedTerms, linkDenylist []string, brands Brands, appNames ...string) *Rules {
	return &Rules{terms: newFilter(blockedTerms, nil), denylist: linkDenylist, brands: brands,
		staffRe: staffClaimRe(appNames, brands), maxLinks: 3, minRepeat: minRepeatWords}
}

// minRepeatWords: repeat rules compare text only for items of at least this
// many words, so "+1" or "thanks" said three times is not spam.
const minRepeatWords = 4

var (
	credentialRe = regexp.MustCompile(`\b(seed|recovery|secret|backup|mnemonic|wallet)\s+(phrase|phrases|words?)\b` +
		`|\bprivate\s+keys?\b` +
		`|\b(2fa|two\s?factor|verification|login|auth|authentication|security|one\s?time|otp|sms|authenticator)\s+codes?\b` +
		`|\b(send|share|give|dm|tell|enter|type|post|need|provide|confirm|verify)\b(\s+\w+){0,3}\s+(password|passwords|passcode|credentials|login\s+details)\b`)
	ethRe    = regexp.MustCompile(`\b0x[0-9a-fA-F]{40}\b`)
	bech32Re = regexp.MustCompile(`(?i)\bbc1[ac-hj-np-z02-9]{11,71}\b`)
	base58Re = regexp.MustCompile(`\b[1-9A-HJ-NP-Za-km-z]{25,44}\b`)

	offPlatformWordsRe = regexp.MustCompile(`\b(dm|pm|message|msg|text|contact|hmu|inbox|whisper)\s+me\b|\bhit\s+me\s+up\b` +
		`|\btelegram\b|\bwhats\s?app\b|\bwechat\b|\bsignal\s+me\b|\badd\s+me\s+on\s+(discord|telegram|whatsapp|snap|snapchat|instagram|insta)\b`)
	offPlatformRawRe = regexp.MustCompile(`(?i)discord(?:app)?\.(?:gg|com/invite)/|\bt\.me/|\bwa\.me/|\btelegram\.me/`)

	mentionRe = regexp.MustCompile(`(?:^|[^\w@/])@([A-Za-z0-9_][A-Za-z0-9_.\-]{1,31})`)
)

// staffClaimRe matches claims to speak for the app ("I'm from <app> support",
// "official <app> team"), run over normalised words.
func staffClaimRe(appNames []string, brands Brands) *regexp.Regexp {
	var alts []string
	seen := map[string]bool{}
	add := func(name string) {
		words := tokens(name)
		if len(words) == 0 {
			return
		}
		for i, w := range words {
			words[i] = regexp.QuoteMeta(w)
		}
		alt := strings.Join(words, `\s+`)
		if !seen[alt] {
			seen[alt] = true
			alts = append(alts, alt)
		}
	}
	for _, n := range appNames {
		add(n)
	}
	for _, b := range append(append([]string{}, brands.long...), brands.short...) {
		add(b)
	}
	if len(alts) == 0 {
		return nil
	}
	names := "(" + strings.Join(alts, "|") + ")"
	return regexp.MustCompile(`\b(i\s+am|i\s+m|im|we\s+are|we\s+re|this\s+is|from|i\s+work\s+(for|at))\s+(the\s+)?` + names +
		`\s+(staff|support|team|admin|admins|administrator|moderator|moderators|mod|mods|helpdesk|help\s+desk|security)\b` +
		`|\b` + names + `\s+(staff|support|admin|administrator|moderator|security|team)\s+(here|speaking|account)\b` +
		`|\bofficial\s+` + names + `\s+(account|support|staff|team|admin)\b` +
		`|\b(i\s+am|i\s+m|im)\s+(an?\s+)?(admin|administrator|moderator|mod)\s+(here|of\s+(the\s+)?` + names + `|on\s+` + names + `|at\s+` + names + `)\b`)
}

func mixedBase58(s string) bool {
	var up, low, dig bool
	for _, r := range s {
		switch {
		case unicode.IsUpper(r):
			up = true
		case unicode.IsLower(r):
			low = true
		case unicode.IsDigit(r):
			dig = true
		}
	}
	return up && low && dig
}

// maskURLs blanks URL spans so address and contact patterns don't fire on
// path segments.
func maskURLs(s string) string {
	return schemeURLRe.ReplaceAllStringFunc(s, func(m string) string { return strings.Repeat(" ", len(m)) })
}

// CountMentions counts distinct @names in text.
func CountMentions(text string) int {
	seen := map[string]bool{}
	for _, m := range mentionRe.FindAllStringSubmatch(text, -1) {
		seen[strings.ToLower(strings.TrimRight(m[1], ".-"))] = true
	}
	return len(seen)
}

// TextHash identifies an item's text for the repeat rules: the normalised
// words of title, body and link, hashed. Empty for items shorter than the
// repeat threshold, which are never compared.
func TextHash(title, body, link string) string {
	words := tokens(title + " " + body + " " + link)
	if len(words) < minRepeatWords {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(words, " ")))
	return hex.EncodeToString(sum[:16])
}

// Evaluate runs rules-v1.
func (r *Rules) Evaluate(in RuleInput) Verdict {
	v := Verdict{Action: DecisionAllow}
	full := strings.TrimSpace(in.Title + "\n" + in.Body)
	all := full
	if in.URL != "" {
		all += "\n" + in.URL
	}
	words := strings.Join(tokens(full), " ")
	raw := strings.ToLower(all)
	f := in.Facts

	if t := r.terms.matchedTerm(all); t != "" {
		v.Rejected = true
		v.add(Hit{Rule: "blocked_term", Action: DecisionRemove, Detail: t, Reason: "Contains language that isn't allowed"})
		return v
	}

	v.Links = ExtractLinks(in.URL, full)
	lookalike, lookAction, shortener := "", DecisionAllow, ""
	for _, l := range v.Links {
		why := l.Lookalike(r.denylist, r.brands)
		if a := lookalikeAction(why); DecisionRank(a) > DecisionRank(lookAction) {
			lookalike, lookAction = safeHost(l.Host)+" ("+why+")", a
		}
		if l.IsShortener() && shortener == "" {
			shortener = l.Domain
		}
	}
	switch lookAction {
	case DecisionRemove:
		v.add(Hit{Rule: "unsafe_link", Action: DecisionRemove, Category: CatScam, Detail: lookalike,
			Reason: "Links to look-alike or unsafe sites aren't allowed", flag: "links to a look-alike or unsafe domain"})
	case DecisionHold:
		v.add(Hit{Rule: "near_brand_link", Action: DecisionHold, Category: CatScam, Detail: lookalike,
			Reason: "Links to sites spelled like this app's are reviewed first",
			flag:   "links to a domain spelled a letter or two away from this app's name"})
	}
	if shortener != "" {
		v.add(Hit{Rule: "link_shortener", Action: DecisionHold, Category: CatSpam, Detail: shortener,
			Reason: "Shortened links are reviewed first", flag: "uses a link shortener"})
	}
	if len(v.Links) > 0 && f.AccountAge < 3*24*time.Hour && (f.Karma == nil || *f.Karma < 10) {
		v.add(Hit{Rule: "new_account_links", Action: DecisionHold, Category: CatSpam, Detail: fmt.Sprintf("%d links", len(v.Links)),
			Reason: "Links from new accounts are reviewed first", flag: "contains links from a new account"})
	}
	if len(v.Links) > r.maxLinks {
		v.add(Hit{Rule: "many_links", Action: DecisionHold, Category: CatSpam, Detail: fmt.Sprintf("%d links", len(v.Links)),
			Reason: "Posts with many links are reviewed first", flag: "contains many links"})
	}
	if m := credentialRe.FindString(words); m != "" {
		v.add(Hit{Rule: "credential_ask", Action: DecisionHold, Category: CatScam, Detail: m,
			Reason: "Mentions of passwords, login codes or recovery phrases are reviewed first",
			flag:   "mentions passwords, login codes, private keys or recovery phrases"})
	}
	masked := maskURLs(all)
	wallet := ethRe.FindString(masked)
	if wallet == "" {
		wallet = bech32Re.FindString(masked)
	}
	if wallet == "" {
		for _, m := range base58Re.FindAllString(masked, -1) {
			if mixedBase58(m) {
				wallet = m
				break
			}
		}
	}
	if wallet != "" {
		v.add(Hit{Rule: "wallet_address", Action: DecisionHold, Category: CatScam, Detail: wallet,
			Reason: "Posts with wallet addresses are reviewed first", flag: "contains a crypto wallet address"})
	}
	if f.AccountAge < 7*24*time.Hour {
		m := offPlatformWordsRe.FindString(words)
		if m == "" {
			m = offPlatformRawRe.FindString(raw)
		}
		if m != "" {
			v.add(Hit{Rule: "off_platform_contact", Action: DecisionHold, Category: CatScam, Detail: m,
				Reason: "Off-platform contact from new accounts is reviewed first",
				flag:   "asks readers to move to private or off-platform contact"})
		}
	}
	if f.AuthorRepeats24h >= 2 { // this is at least the third copy
		v.add(Hit{Rule: "repeat_self", Action: DecisionRemove, Category: CatSpam, Detail: fmt.Sprintf("%d earlier copies", f.AuthorRepeats24h),
			Reason: "Posting the same text again and again isn't allowed", flag: "repeats the author's own text"})
	}
	if f.OtherAccounts24h >= 3 {
		v.add(Hit{Rule: "repeat_accounts", Action: DecisionHold, Category: CatManipulation, Detail: fmt.Sprintf("%d other accounts", f.OtherAccounts24h),
			Reason: "Text posted by several accounts is reviewed first", flag: "was posted word for word by several accounts"})
	}
	v.Mentions = CountMentions(full)
	if v.Mentions > 10 {
		v.add(Hit{Rule: "mention_flood", Action: DecisionHold, Category: CatHarassment, Detail: fmt.Sprintf("%d mentions", v.Mentions),
			Reason: "Posts that mention many people are reviewed first", flag: "mentions many people"})
	}
	if !f.Staff && r.staffRe != nil {
		if m := r.staffRe.FindString(words); m != "" {
			v.add(Hit{Rule: "staff_impersonation", Action: DecisionHold, Category: CatScam, Detail: m,
				Reason: "Claims to be staff are reviewed first", flag: "claims to be this app's staff or support"})
		}
	}
	return v
}
