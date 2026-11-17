package moderation

import (
	"net"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Link is one URL found in an item: written with a scheme, as a Markdown
// link, or bare ("bit.ly/x", "discord.gg/abc").
type Link struct {
	URL    string // as found (a scheme added to bare ones)
	Host   string // lowercase; punycode labels kept as xn--
	Domain string // registrable domain (approximate public-suffix rule)
	IP     bool   // an IP literal or a numeric host
	IDN    bool   // an internationalised name (punycode or non-ASCII)
}

var (
	schemeURLRe = regexp.MustCompile(`(?i)\bhttps?://[^\s<>()\[\]"'` + "`" + `]+`)
	// Bare domains need a known TLD so ordinary words ("e.g.", "v1.2") don't count.
	bareURLRe = regexp.MustCompile(`(?i)(?:^|[\s(\[<"'*_])((?:[a-z0-9\p{L}](?:[a-z0-9\p{L}-]{0,61}[a-z0-9\p{L}])?\.)+` +
		`(?:com|net|org|io|gg|ly|co|me|xyz|ru|info|biz|app|dev|site|online|shop|store|link|click|top|tk|ml|ga|cf|gq|cc|to|us|uk|de|fr|cn|tv|ai|so|vip|live|pro|fun|world|trade|club|at|in|is|it|nl|be|eu|ws|su|pw|gl|im|li|sh|lol)` +
		`(?:/[^\s<>()\[\]"'` + "`" + `]*)?)`)
)

// twoPartSuffixes are public suffixes with two labels, enough to find the
// registrable domain of common hosts without the full public-suffix list.
var twoPartSuffixes = map[string]bool{
	"co.uk": true, "org.uk": true, "ac.uk": true, "gov.uk": true, "me.uk": true,
	"com.au": true, "net.au": true, "org.au": true, "co.jp": true, "ne.jp": true, "or.jp": true,
	"com.br": true, "com.cn": true, "co.nz": true, "co.in": true, "co.za": true, "com.mx": true,
	"com.tr": true, "com.sg": true, "com.hk": true, "co.kr": true, "com.ar": true,
}

// ExtractLinks finds every distinct link in texts.
func ExtractLinks(texts ...string) []Link {
	seen := map[string]bool{}
	var out []Link
	add := func(raw string) {
		raw = strings.TrimRight(raw, ".,;:!?*_")
		l, ok := ParseLink(raw)
		if !ok {
			return
		}
		key := l.Host + strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(l.URL, "https://"), "http://"))
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, l)
	}
	for _, t := range texts {
		if t == "" {
			continue
		}
		spans := schemeURLRe.FindAllStringIndex(t, -1)
		for _, sp := range spans {
			add(t[sp[0]:sp[1]])
		}
		// bare domains outside the scheme URLs already found
		masked := []byte(t)
		for _, sp := range spans {
			for i := sp[0]; i < sp[1]; i++ {
				masked[i] = ' '
			}
		}
		for _, m := range bareURLRe.FindAllStringSubmatch(string(masked), -1) {
			add("http://" + m[1])
		}
	}
	return out
}

// ParseLink parses one URL (a scheme is added when missing).
func ParseLink(raw string) (Link, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Link{}, false
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return Link{}, false
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" {
		return Link{}, false
	}
	l := Link{URL: raw, Host: host}
	if ip := net.ParseIP(host); ip != nil || numericHost(host) {
		l.IP = true
		l.Domain = host
		return l, true
	}
	for _, lab := range strings.Split(host, ".") {
		if strings.HasPrefix(lab, "xn--") {
			l.IDN = true
		}
	}
	for _, r := range host {
		if r > unicode.MaxASCII {
			l.IDN = true
			break
		}
	}
	l.Domain = registrable(host)
	return l, true
}

// numericHost catches IPv4 written as one number or in hex ("3232235777", "0xC0A80001").
func numericHost(h string) bool {
	if strings.HasPrefix(h, "0x") {
		return len(h) > 2 && strings.Trim(h[2:], "0123456789abcdef.") == ""
	}
	return strings.Trim(h, "0123456789.") == "" && strings.Count(h, ".") != 3 // dotted quads parse as IPs above
}

func registrable(host string) string {
	labels := strings.Split(host, ".")
	n := len(labels)
	if n <= 2 {
		return host
	}
	if twoPartSuffixes[labels[n-2]+"."+labels[n-1]] {
		return strings.Join(labels[n-3:], ".")
	}
	return strings.Join(labels[n-2:], ".")
}

// ---- classification ----

// shorteners are link shorteners (rule link_shortener).
var shorteners = map[string]bool{
	"bit.ly": true, "bitly.com": true, "tinyurl.com": true, "t.co": true, "goo.gl": true, "is.gd": true,
	"cutt.ly": true, "rebrand.ly": true, "ow.ly": true, "buff.ly": true, "tiny.cc": true, "shorturl.at": true,
	"rb.gy": true, "s.id": true, "bl.ink": true, "t.ly": true, "v.gd": true, "lnkd.in": true, "adf.ly": true,
	"shorte.st": true, "tr.im": true, "soo.gd": true, "short.io": true, "tiny.one": true,
}

// IsShortener reports a link shortener.
func (l Link) IsShortener() bool { return shorteners[l.Domain] || shorteners[l.Host] }

// Brands are the names look-alike domains imitate: the app's own domains
// (PUBLIC_WEB_URL and MODERATION_BRAND_DOMAINS). Names of 5 letters or more
// are checked for visual copies, near spellings and brand-in-subdomain
// tricks; every name is checked against the skeleton of internationalised
// names.
type Brands struct {
	official map[string]bool // registrable domains that are the real thing
	long     []string
	short    []string
}

// NewBrands builds the brand list from official domains ("example.com").
func NewBrands(domains ...string) Brands {
	b := Brands{official: map[string]bool{}}
	seen := map[string]bool{}
	for _, d := range domains {
		d = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(d)), ".")
		if d == "" || net.ParseIP(d) != nil || !strings.Contains(d, ".") {
			continue
		}
		reg := registrable(d)
		b.official[reg] = true
		label := strings.Split(reg, ".")[0]
		if label == "" || seen[label] {
			continue
		}
		seen[label] = true
		if len([]rune(label)) >= 5 {
			b.long = append(b.long, label)
		} else {
			b.short = append(b.short, label)
		}
	}
	return b
}

// Official reports whether domain is one of the app's own.
func (b Brands) Official(domain string) bool { return b.official[domain] }

// Why a link is unsafe (Link.Lookalike). NearBrand is the only one that is
// held rather than removed: a near spelling is also how many real sites are
// named, so a person decides.
const (
	WhyIPLiteral  = "ip_literal"
	WhyDenylisted = "denylisted"
	WhyIDN        = "idn_lookalike"   // an internationalised name whose skeleton holds a brand
	WhyVisualCopy = "visual_copy"     // reads as the brand ("examp1e.com", the brand on another TLD)
	WhyBrandLabel = "brand_imitation" // the brand as a label of someone else's domain ("example.com.evil.ru")
	WhyNearBrand  = "near_brand"      // a letter or two from a brand, and not a visual copy
)

// lookalikeAction is the action for a Lookalike reason.
func lookalikeAction(why string) string {
	switch why {
	case "":
		return DecisionAllow
	case WhyNearBrand:
		return DecisionHold
	}
	return DecisionRemove
}

// confusables maps characters that look like ASCII letters to them.
var confusables = map[rune]rune{
	// Cyrillic
	'а': 'a', 'в': 'b', 'е': 'e', 'ё': 'e', 'о': 'o', 'р': 'p', 'с': 'c', 'у': 'y', 'х': 'x', 'і': 'i', 'ї': 'i',
	'ј': 'j', 'ѕ': 's', 'ԁ': 'd', 'ӏ': 'l', 'һ': 'h', 'ԛ': 'q', 'ԝ': 'w', 'к': 'k', 'м': 'm', 'т': 't', 'п': 'n',
	'г': 'r', 'ь': 'b', 'ц': 'u', 'ɡ': 'g',
	// Greek
	'α': 'a', 'β': 'b', 'ε': 'e', 'ι': 'i', 'κ': 'k', 'ν': 'v', 'ο': 'o', 'ρ': 'p', 'τ': 't', 'υ': 'u', 'χ': 'x',
	'ω': 'w', 'μ': 'm', 'η': 'n',
	// Latin extras and digits used as letters
	'ı': 'i', 'ł': 'l', 'ƚ': 'l', 'ȷ': 'j', '0': 'o', '1': 'l', '3': 'e', '5': 's', '7': 't', '@': 'a',
}

// skeleton is a label with accents stripped and look-alike characters mapped
// to ASCII (Cyrillic "ехаmрlе" → "example").
func skeleton(label string) string {
	var b strings.Builder
	for _, r := range norm.NFKD.String(strings.ToLower(label)) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if m, ok := confusables[r]; ok {
			r = m
		}
		b.WriteRune(r)
	}
	return b.String()
}

// visualReplacer folds letter shapes a reader can't tell apart in a URL bar:
// i and l (and 1, mapped to l by skeleton), rn and m, vv and w.
var visualReplacer = strings.NewReplacer("rn", "m", "vv", "w", "i", "l")

// visual is how a label reads: its skeleton with look-alike letter shapes
// folded, so "exarnple" and "examp1e" read as "example".
func visual(label string) string { return visualReplacer.Replace(skeleton(label)) }

// Lookalike explains why a link imitates a brand or is otherwise unsafe (the
// Why* reasons), or returns "".
func (l Link) Lookalike(denylist []string, brands Brands) string {
	if l.IP {
		return WhyIPLiteral
	}
	for _, d := range denylist {
		d = strings.ToLower(strings.TrimSpace(d))
		if d != "" && (l.Host == d || strings.HasSuffix(l.Host, "."+d)) {
			return WhyDenylisted
		}
	}
	if brands.official[l.Domain] {
		return ""
	}
	labels := strings.Split(l.Host, ".")
	if l.IDN {
		for _, lab := range labels {
			u := lab
			if strings.HasPrefix(lab, "xn--") {
				if dec, ok := punyDecode(lab[4:]); ok {
					u = dec
				}
			}
			sk := visual(u)
			for _, b := range append(append([]string{}, brands.long...), brands.short...) {
				if strings.Contains(sk, visual(b)) {
					return WhyIDN
				}
			}
		}
	}
	regLabel := visual(strings.Split(l.Domain, ".")[0])
	near := false
	for _, b := range brands.long {
		// the official domain returned above, so reading as the brand is an
		// imitation ("examp1e.com", "example.net")
		vb := visual(b)
		if regLabel == vb {
			return WhyVisualCopy
		}
		for _, lab := range labels {
			if v := visual(lab); v == vb || strings.HasPrefix(v, vb+"-") || strings.HasSuffix(v, "-"+vb) || strings.Contains(v, "-"+vb+"-") {
				return WhyBrandLabel
			}
		}
		limit := 1
		if len([]rune(vb)) >= 8 {
			limit = 2
		}
		if levenshtein(regLabel, vb) <= limit {
			near = true
		}
	}
	if near {
		return WhyNearBrand
	}
	return ""
}

// knownSites describe common domains in the decision model's links feature.
var knownSites = map[string]string{
	"youtube.com": "a video site", "youtu.be": "a video site", "vimeo.com": "a video site", "twitch.tv": "a streaming site",
	"x.com": "a social network", "twitter.com": "a social network", "facebook.com": "a social network",
	"instagram.com": "a social network", "linkedin.com": "a professional network", "reddit.com": "a forum site",
	"github.com": "a code hosting site", "gitlab.com": "a code hosting site", "wikipedia.org": "an encyclopedia",
	"medium.com": "a blogging site", "substack.com": "a newsletter site", "google.com": "a search engine",
	"discord.gg": "a chat invite link", "discord.com": "a chat site",
	"t.me": "a Telegram chat link", "telegram.me": "a Telegram chat link", "wa.me": "a WhatsApp chat link",
}

// describe is the link in the decision model's words: the host from the URL
// parser (only [a-z0-9.-], punycode left as xn--) and what kind of site it is.
func (l Link) describe(denylist []string, brands Brands) string {
	host := safeHost(l.Host)
	switch {
	case l.IP:
		return "an IP address instead of a domain name"
	case l.IsShortener():
		return host + " (a link shortener)"
	case brands.official[l.Domain]:
		return host + " (this app's own site)"
	}
	switch why := l.Lookalike(denylist, brands); why {
	case "":
	case WhyNearBrand:
		return host + " (a domain spelled a letter or two away from this app's name)"
	default:
		return host + " (a domain that imitates this app or is on the deny list)"
	}
	if d, ok := knownSites[l.Host]; ok {
		return host + " (" + d + ")"
	}
	if d, ok := knownSites[l.Domain]; ok {
		return safeHost(l.Domain) + " (" + d + ")"
	}
	if l.IDN {
		return host + " (an internationalised domain name)"
	}
	return host + " (an unknown site)"
}

// safeHost keeps a host to ASCII letters, digits, dots and hyphens (≤ 60).
func safeHost(h string) string {
	var b strings.Builder
	for _, r := range h {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-' {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if len(s) > 60 {
		s = s[:60]
	}
	return s
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// punyDecode decodes one punycode label body (after "xn--"), RFC 3492.
func punyDecode(s string) (string, bool) {
	const (
		base, tmin, tmax, skew, damp = 36, 1, 26, 38, 700
		initialBias, initialN        = 72, 128
	)
	adapt := func(delta, numPoints int, first bool) int {
		if first {
			delta /= damp
		} else {
			delta /= 2
		}
		delta += delta / numPoints
		k := 0
		for delta > ((base-tmin)*tmax)/2 {
			delta /= base - tmin
			k += base
		}
		return k + (base-tmin+1)*delta/(delta+skew)
	}
	var out []rune
	in := s
	if i := strings.LastIndexByte(s, '-'); i >= 0 {
		for _, r := range s[:i] {
			if r > unicode.MaxASCII {
				return "", false
			}
			out = append(out, r)
		}
		in = s[i+1:]
	}
	n, i, bias := initialN, 0, initialBias
	for p := 0; p < len(in); {
		oldi, w := i, 1
		for k := base; ; k += base {
			if p >= len(in) {
				return "", false
			}
			c := in[p]
			p++
			var digit int
			switch {
			case c >= '0' && c <= '9':
				digit = int(c-'0') + 26
			case c >= 'a' && c <= 'z':
				digit = int(c - 'a')
			case c >= 'A' && c <= 'Z':
				digit = int(c - 'A')
			default:
				return "", false
			}
			i += digit * w
			if i < 0 || i > 1<<24 {
				return "", false
			}
			t := k - bias
			if t < tmin {
				t = tmin
			} else if t > tmax {
				t = tmax
			}
			if digit < t {
				break
			}
			w *= base - t
		}
		bias = adapt(i-oldi, len(out)+1, oldi == 0)
		n += i / (len(out) + 1)
		i %= len(out) + 1
		if n > unicode.MaxRune {
			return "", false
		}
		out = append(out, 0)
		copy(out[i+1:], out[i:])
		out[i] = rune(n)
		i++
	}
	return string(out), true
}
