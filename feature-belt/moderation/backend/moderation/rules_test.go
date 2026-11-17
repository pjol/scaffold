package moderation

import (
	"slices"
	"testing"
	"time"
)

func karma(n int) *int { return &n }

// established is an author no age or karma rule applies to.
var established = Facts{AccountAge: 400 * 24 * time.Hour, Karma: karma(500)}

func newAuthor() Facts { return Facts{AccountAge: 2 * time.Hour} }

// TestRules is the rules-v1 table: every rule fires on its case and stays
// quiet on its near miss.
func TestRules(t *testing.T) {
	r := NewRules([]string{"scamword", "very bad phrase"}, []string{"evil.example"},
		NewBrands("acmeforum.com", "acme.app"), "Acme Forum")
	cases := []struct {
		name     string
		in       RuleInput
		rule     string // must fire
		action   string // the verdict's action
		category string
		absent   string // must not fire
	}{
		{name: "blocked term (leetspeak)", in: RuleInput{Body: "what a sc4mw0rd deal", Facts: established}, rule: "blocked_term", action: DecisionRemove},
		{name: "blocked phrase spaced out", in: RuleInput{Title: "a very  bad   phrase", Facts: established}, rule: "blocked_term", action: DecisionRemove},
		{name: "blocked term near miss", in: RuleInput{Body: "scamwords are fine to discuss", Facts: established}, absent: "blocked_term", action: DecisionAllow},

		{name: "brand in subdomain", in: RuleInput{Body: "deal at https://acmeforum.com.evil.ru/deal", Facts: established},
			rule: "unsafe_link", action: DecisionRemove, category: CatScam},
		{name: "IP literal", in: RuleInput{URL: "http://192.168.10.4/login", Facts: established}, rule: "unsafe_link", action: DecisionRemove},
		{name: "numeric host", in: RuleInput{Body: "http://3232235777/login", Facts: established}, rule: "unsafe_link", action: DecisionRemove},
		{name: "Cyrillic look-alike", in: RuleInput{Body: "https://аcmeforum.com/free", Facts: established}, rule: "unsafe_link", action: DecisionRemove},
		{name: "typosquat", in: RuleInput{Body: "log in at acmef0rum.com/login", Facts: established}, rule: "unsafe_link", action: DecisionRemove},
		{name: "brand on another TLD", in: RuleInput{Body: "https://acmeforum.net/login", Facts: established}, rule: "unsafe_link", action: DecisionRemove},
		{name: "denylist", in: RuleInput{Body: "see https://cards.evil.example/x", Facts: established}, rule: "unsafe_link", action: DecisionRemove},
		{name: "near spelling is only held", in: RuleInput{Body: "art by https://acmeforms.com/gallery", Facts: established},
			rule: "near_brand_link", action: DecisionHold, category: CatScam},
		{name: "official domain", in: RuleInput{Body: "https://www.acmeforum.com/rules and https://acme.app/x", Facts: established},
			absent: "unsafe_link", action: DecisionAllow},

		{name: "shortener", in: RuleInput{Body: "prices here bit.ly/3xYz", Facts: established}, rule: "link_shortener", action: DecisionHold, category: CatSpam},
		{name: "shortener near miss", in: RuleInput{Body: "see wikipedia.org/wiki/Bread", Facts: established}, absent: "link_shortener", action: DecisionAllow},

		{name: "link from a new account", in: RuleInput{Body: "my work https://example.org/a", Facts: Facts{AccountAge: 24 * time.Hour, Karma: karma(3)}},
			rule: "new_account_links", action: DecisionHold},
		{name: "link from a new account without karma tracking", in: RuleInput{Body: "https://example.org/a", Facts: Facts{AccountAge: time.Hour}},
			rule: "new_account_links", action: DecisionHold},
		{name: "new account with karma", in: RuleInput{Body: "https://example.org/a", Facts: Facts{AccountAge: 24 * time.Hour, Karma: karma(12)}},
			absent: "new_account_links", action: DecisionAllow},

		{name: "many links", in: RuleInput{Body: "https://a.com https://b.com https://c.com https://d.com", Facts: established},
			rule: "many_links", action: DecisionHold},
		{name: "three links", in: RuleInput{Body: "https://a.com https://b.com https://c.com", Facts: established}, absent: "many_links", action: DecisionAllow},

		{name: "seed phrase", in: RuleInput{Body: "To verify your wallet, reply with your seed phrase", Facts: established},
			rule: "credential_ask", action: DecisionHold, category: CatScam},
		{name: "2FA code", in: RuleInput{Body: "dm me the 2FA code you just got", Facts: established}, rule: "credential_ask", action: DecisionHold},
		{name: "password ask", in: RuleInput{Body: "just send me your password and I'll fix it", Facts: established}, rule: "credential_ask", action: DecisionHold},
		{name: "credential near miss", in: RuleInput{Body: "the seed of this idea is simple", Facts: established}, absent: "credential_ask", action: DecisionAllow},

		{name: "ETH address", in: RuleInput{Body: "send to 0x52908400098527886E0F7030069857D2E4169EE7", Facts: established},
			rule: "wallet_address", action: DecisionHold},
		{name: "BTC bech32", in: RuleInput{Body: "tips: bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq", Facts: established}, rule: "wallet_address", action: DecisionHold},
		{name: "wallet near miss", in: RuleInput{Body: "it sold at 0x12 the usual, 100 units", Facts: established}, absent: "wallet_address", action: DecisionAllow},

		{name: "DM me from a new account", in: RuleInput{Body: "DM me for a better price", Facts: newAuthor()}, rule: "off_platform_contact", action: DecisionHold},
		{name: "chat invite from a new account", in: RuleInput{Body: "join discord.gg/abcd", Facts: Facts{AccountAge: 5 * 24 * time.Hour}}, rule: "off_platform_contact", action: DecisionHold},
		{name: "DM me from an older account", in: RuleInput{Body: "DM me for a better price", Facts: established}, absent: "off_platform_contact", action: DecisionAllow},

		{name: "third copy by the author", in: RuleInput{Body: "same text again and again", Facts: Facts{AccountAge: 400 * 24 * time.Hour, AuthorRepeats24h: 2}},
			rule: "repeat_self", action: DecisionRemove, category: CatSpam},
		{name: "second copy", in: RuleInput{Body: "same text again", Facts: Facts{AccountAge: 400 * 24 * time.Hour, AuthorRepeats24h: 1}},
			absent: "repeat_self", action: DecisionAllow},
		{name: "several accounts", in: RuleInput{Body: "same text", Facts: Facts{AccountAge: 400 * 24 * time.Hour, OtherAccounts24h: 3}},
			rule: "repeat_accounts", action: DecisionHold, category: CatManipulation},

		{name: "mention flood", in: RuleInput{Body: "@a1 @b2 @c3 @d4 @e5 @f6 @g7 @h8 @i9 @j10 @k11", Facts: established}, rule: "mention_flood", action: DecisionHold},
		{name: "a few mentions", in: RuleInput{Body: "@sam @jordan thoughts?", Facts: established}, absent: "mention_flood", action: DecisionAllow},
		{name: "email is not a mention", in: RuleInput{Body: "write to help@example.org", Facts: established}, absent: "mention_flood", action: DecisionAllow},

		{name: "staff impersonation", in: RuleInput{Body: "Hi, I'm from Acme Forum support, verify here", Facts: established},
			rule: "staff_impersonation", action: DecisionHold, category: CatScam},
		{name: "official team claim", in: RuleInput{Body: "This is the official acme forum team", Facts: established}, rule: "staff_impersonation", action: DecisionHold},
		{name: "brand label claim", in: RuleInput{Body: "acmeforum staff here, reset your account", Facts: established}, rule: "staff_impersonation", action: DecisionHold},
		{name: "staff may say it", in: RuleInput{Body: "I'm from Acme Forum support", Facts: Facts{AccountAge: time.Hour * 9000, Staff: true}},
			absent: "staff_impersonation", action: DecisionAllow},
		{name: "talking about support", in: RuleInput{Body: "Acme Forum support answered my ticket quickly", Facts: established}, absent: "staff_impersonation", action: DecisionAllow},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := r.Evaluate(c.in)
			if c.rule != "" && !hasRule(v, c.rule) {
				t.Fatalf("rule %s didn't fire: %+v", c.rule, v.Hits)
			}
			if c.absent != "" && hasRule(v, c.absent) {
				t.Fatalf("rule %s fired: %+v", c.absent, v.Hits)
			}
			if v.Action != c.action {
				t.Fatalf("action %s, want %s (%+v)", v.Action, c.action, v.Hits)
			}
			if c.category != "" && !slices.Contains(v.Categories, c.category) {
				t.Fatalf("categories %v, want %s", v.Categories, c.category)
			}
		})
	}
}

func hasRule(v Verdict, rule string) bool {
	return slices.ContainsFunc(v.Hits, func(h Hit) bool { return h.Rule == rule })
}

func TestBlockedTermIsRejected(t *testing.T) {
	r := NewRules([]string{"scamword"}, nil, Brands{})
	v := r.Evaluate(RuleInput{Body: "S C A M W O R D", Facts: established})
	if !v.Rejected {
		t.Fatalf("expected a rejection, got %+v", v)
	}
}

func TestTextHash(t *testing.T) {
	if TextHash("", "+1", "") != "" {
		t.Fatal("short texts must not be compared")
	}
	a := TextHash("Hello", "This is the same TEXT here", "")
	b := TextHash("hello", "this is the same text   here!", "")
	if a == "" || a != b {
		t.Fatalf("normalised copies must hash the same: %q %q", a, b)
	}
	if a == TextHash("Hello", "This is a different text here", "") {
		t.Fatal("different texts hashed the same")
	}
}

func TestPunyDecode(t *testing.T) {
	got, ok := punyDecode("mnchen-3ya")
	if !ok || got != "münchen" {
		t.Fatalf("punyDecode = %q, %v", got, ok)
	}
}

func TestLinksExtracted(t *testing.T) {
	ls := ExtractLinks("", "see [this](https://example.org/a), bit.ly/x and e.g. v1.2 or https://example.org/a")
	if len(ls) != 2 {
		t.Fatalf("links %+v", ls)
	}
}
