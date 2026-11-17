package aauth

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func wantInvalidRequest(t *testing.T, err error) {
	t.Helper()
	var p *Problem
	if !errors.As(err, &p) || p.Code != CodeInvalidRequest {
		t.Fatalf("want invalid_request, got %v", err)
	}
}

// protocol, Mission Approval, and "s256 ... over the exact bytes it persists".
func TestMissionBlob(t *testing.T) {
	approved := time.Date(2026, 4, 7, 14, 30, 0, 0, time.UTC)
	built, err := BuildMissionBlob(MissionBlobInput{
		Agent:             "aauth:assistant@agent.example",
		ApprovedAt:        approved,
		ExpiresAt:         approved.AddDate(0, 1, 0),
		Description:       "# Plan Japan Vacation",
		ApprovedTools:     []Tool{{Name: "WebSearch", Description: "Search the web"}},
		ApprovedResources: []string{"https://flights.example"},
		Extra:             map[string]json.RawMessage{"future_member": json.RawMessage(`{"a":1}`)},
	}, IdentifierPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"agent":"aauth:assistant@agent.example","approved_at":"2026-04-07T14:30:00Z","expires_at":"2026-05-07T14:30:00Z","description":"# Plan Japan Vacation","approved_tools":[{"name":"WebSearch","description":"Search the web"}],"approved_resources":["https://flights.example"],"future_member":{"a":1}}`
	if string(built.Bytes) != want {
		t.Fatalf("bytes:\n%s", built.Bytes)
	}
	if built.S256 != S256(built.Bytes) || len(built.S256) != 43 || strings.ContainsAny(built.S256, "+/=") {
		t.Errorf("s256 %s", built.S256)
	}
	if built.Encoded != Base64URL(built.Bytes) || strings.Contains(built.Encoded, "=") {
		t.Errorf("encoded %s", built.Encoded)
	}
	if !ValidMissionS256(built.S256) || ValidMissionS256("short") || ValidMissionS256(strings.Repeat("!", 43)) {
		t.Error("ValidMissionS256")
	}
	// The verification an agent and an SS make.
	raw, err := DecodeMission(built.Encoded, built.S256)
	if err != nil || string(raw) != string(built.Bytes) {
		t.Fatalf("%s %v", raw, err)
	}
	if _, err := DecodeMission(built.Encoded, S256([]byte("other"))); err == nil {
		t.Error("a wrong s256 passed")
	}
	if _, err := DecodeMission("***", built.S256); err == nil {
		t.Error("bad encoding passed")
	}
	// A reader keeps what it doesn't recognize, and reads the times.
	blob, err := ParseMissionBlob(raw)
	if err != nil {
		t.Fatal(err)
	}
	if blob.Agent != "aauth:assistant@agent.example" || string(blob.Extra["future_member"]) != `{"a":1}` || len(blob.ApprovedTools) != 1 {
		t.Fatalf("%+v", blob)
	}
	exp, ok, err := blob.ExpiresTime()
	if err != nil || !ok || !exp.Equal(approved.AddDate(0, 1, 0)) {
		t.Fatalf("%s %v %v", exp, ok, err)
	}
	// With no expires_at and no options, a minimal blob.
	minimal, err := BuildMissionBlob(MissionBlobInput{Agent: "aauth:a@agent.example", ApprovedAt: approved, Description: "d"}, IdentifierPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if string(minimal.Bytes) != `{"agent":"aauth:a@agent.example","approved_at":"2026-04-07T14:30:00Z","description":"d"}` {
		t.Fatalf("%s", minimal.Bytes)
	}
	mb, _ := ParseMissionBlob(minimal.Bytes)
	if _, ok, err := mb.ExpiresTime(); ok || err != nil {
		t.Errorf("%v %v", ok, err)
	}
	// Required members.
	for name, in := range map[string]MissionBlobInput{
		"no agent":       {ApprovedAt: approved, Description: "d"},
		"bad agent":      {Agent: "x", ApprovedAt: approved, Description: "d"},
		"no approved_at": {Agent: "aauth:a@agent.example", Description: "d"},
		"no description": {Agent: "aauth:a@agent.example", ApprovedAt: approved},
	} {
		if _, err := BuildMissionBlob(in, IdentifierPolicy{}); err == nil {
			t.Errorf("%s: built", name)
		}
	}
	if _, err := ParseMissionBlob([]byte(`{"agent":"a"}`)); err == nil {
		t.Error("an incomplete blob parsed")
	}
	if _, err := ParseMissionBlob([]byte(`x`)); err == nil {
		t.Error("not JSON parsed")
	}
	// The approval response's members, with capabilities outside the blob.
	b := jsonBytes(t, MissionApproval{S256: built.S256, Mission: built.Encoded, Capabilities: []string{"interaction"}, PersonTokens: map[string]string{"https://flights.example": "eyJ"}})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	for _, k := range []string{"s256", "mission", "capabilities", "person_tokens"} {
		if _, ok := m[k]; !ok {
			t.Errorf("no %s in %s", k, b)
		}
	}
}

func TestProtocolRequestValidation(t *testing.T) {
	p := IdentifierPolicy{}
	good := S256([]byte("m"))
	// Person token request.
	if err := (&PersonTokenRequest{Resource: testResource, MissionS256: good}).Validate(p); err != nil {
		t.Error(err)
	}
	for _, q := range []PersonTokenRequest{
		{},
		{Resource: "http://resource.example"},
		{Resource: "https://resource.example/path"},
		{Resource: testResource, MissionS256: "short"},
		{Resource: testResource, MissionS256: good, TokenRequestCommon: TokenRequestCommon{UpstreamToken: "x"}},
		{Resource: testResource, TokenRequestCommon: TokenRequestCommon{Platform: "toaster"}},
		{Resource: testResource, TokenRequestCommon: TokenRequestCommon{Device: strings.Repeat("d", 65)}},
		{Resource: testResource, TokenRequestCommon: TokenRequestCommon{Device: "a\x01b"}},
	} {
		q := q
		wantInvalidRequest(t, q.Validate(p))
	}
	ok := PersonTokenRequest{Resource: testResource, TokenRequestCommon: TokenRequestCommon{Platform: PlatformSelfHosted, Device: "Pixel 8 (App)", Capabilities: []string{"interaction"}}}
	if err := ok.Validate(p); err != nil {
		t.Error(err)
	}
	// The body decodes in its flat wire form, and unknown members are ignored.
	var decoded PersonTokenRequest
	if err := json.Unmarshal([]byte(`{"resource":"https://resource.example","capabilities":["interaction"],"upstream_token":"u","future":1}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.UpstreamToken != "u" || decoded.Capabilities[0] != "interaction" {
		t.Errorf("%+v", decoded)
	}
	// Auth token request: "presented_token (REQUIRED)".
	if err := (&AuthTokenRequest{ResourceToken: "r", PresentedToken: "p"}).Validate(); err != nil {
		t.Error(err)
	}
	wantInvalidRequest(t, (&AuthTokenRequest{ResourceToken: "r"}).Validate())
	wantInvalidRequest(t, (&AuthTokenRequest{PresentedToken: "p"}).Validate())
	// Pending actions.
	if err := (&PendingAction{Action: PendingActionClarificationResponse, ClarificationResponse: "x"}).Validate(); err != nil {
		t.Error(err)
	}
	if err := (&PendingAction{Action: PendingActionUpdatedRequest, ResourceToken: "r", PresentedToken: "p"}).Validate(); err != nil {
		t.Error(err)
	}
	for _, a := range []PendingAction{{}, {Action: "other"}, {Action: PendingActionClarificationResponse}, {Action: PendingActionUpdatedRequest, ResourceToken: "r"}} {
		a := a
		wantInvalidRequest(t, a.Validate())
	}
	// Mission actions: "a missing or unrecognized action with 400".
	if err := (&MissionActionRequest{Action: MissionActionUpdate, Description: "d"}).Validate(); err != nil {
		t.Error(err)
	}
	if err := (&MissionActionRequest{Action: MissionActionCompletion, Summary: "s"}).Validate(); err != nil {
		t.Error(err)
	}
	for _, a := range []MissionActionRequest{{}, {Action: "approved"}, {Action: MissionActionUpdate}, {Action: MissionActionCompletion}} {
		a := a
		wantInvalidRequest(t, a.Validate())
	}
	// Mission proposals.
	if err := (&MissionProposal{Description: "d", Resources: []string{testResource}}).Validate(p); err != nil {
		t.Error(err)
	}
	wantInvalidRequest(t, (&MissionProposal{}).Validate(p))
	wantInvalidRequest(t, (&MissionProposal{Description: "d", Resources: []string{"http://x.example"}}).Validate(p))
	// Permission and audit.
	if err := (&PermissionRequest{Action: "SendEmail", Parameters: json.RawMessage(`{"to":"a"}`)}).Validate(); err != nil {
		t.Error(err)
	}
	wantInvalidRequest(t, (&PermissionRequest{}).Validate())
	wantInvalidRequest(t, (&PermissionRequest{Action: "a", MissionS256: "short"}).Validate())
	wantInvalidRequest(t, (&PermissionRequest{Action: "a", Parameters: json.RawMessage(`[1]`)}).Validate())
	if err := (&AuditRequest{MissionS256: good, Action: "a", Result: json.RawMessage(`{"status":"completed"}`)}).Validate(); err != nil {
		t.Error(err)
	}
	wantInvalidRequest(t, (&AuditRequest{Action: "a"}).Validate()) // "It requires a mission"
	wantInvalidRequest(t, (&AuditRequest{MissionS256: good}).Validate())
	wantInvalidRequest(t, (&AuditRequest{MissionS256: good, Action: "a", Result: json.RawMessage(`"x"`)}).Validate())
	// Interaction: "One of interaction, payment, or question"; PS-95 removes completion.
	if err := (&InteractionRequest{Type: InteractionTypeInteraction, URL: "https://booking.example/confirm", Code: "X7K2-M9P4"}).Validate(p); err != nil {
		t.Error(err)
	}
	if err := (&InteractionRequest{Type: InteractionTypeQuestion, Question: "?"}).Validate(p); err != nil {
		t.Error(err)
	}
	for _, q := range []InteractionRequest{
		{},
		{Type: "completion"},
		{Type: InteractionTypeQuestion},
		{Type: InteractionTypePayment, URL: "http://a.example/pay"},
		{Type: InteractionTypeInteraction, URL: "https://a.example/i?x=1"},
		{Type: InteractionTypeQuestion, Question: "?", MissionS256: "short"},
	} {
		q := q
		wantInvalidRequest(t, q.Validate(p))
	}
	// Revocation.
	if err := (&RevocationRequest{Jti: "j", Exp: 1}).Validate(); err != nil {
		t.Error(err)
	}
	wantInvalidRequest(t, (&RevocationRequest{Exp: 1}).Validate())
	wantInvalidRequest(t, (&RevocationRequest{Jti: "j"}).Validate())
}

// protocol, Revocation Response: the caller's reading of a 200.
func TestParseRevocationResponse(t *testing.T) {
	if r, ok := ParseRevocationResponse(nil); !ok || r.Downstream != nil {
		t.Errorf("empty: %+v %v", r, ok)
	}
	r, ok := ParseRevocationResponse([]byte(`{"downstream":[{"recipient":"https://resource.example","error":"revocation_unavailable"},{"recipient":"https://b.example"}]}`))
	if !ok || len(r.Downstream) != 2 || r.Downstream[0].Error != RevocationUnavailable || r.Downstream[1].Error != "" {
		t.Errorf("%+v %v", r, ok)
	}
	if r, ok := ParseRevocationResponse([]byte(`{}`)); !ok || r.Downstream != nil {
		t.Errorf("no member: %+v %v", r, ok)
	}
	for _, bad := range []string{"not json", "[1,2]", `{"downstream":"x"}`} {
		if _, ok := ParseRevocationResponse([]byte(bad)); ok {
			t.Errorf("%s parsed", bad)
		}
	}
	// A successful revocation's entry has no error member on the wire.
	if b := jsonBytes(t, DownstreamRevocation{Recipient: "https://a.example"}); string(b) != `{"recipient":"https://a.example"}` {
		t.Errorf("%s", b)
	}
}

// The R3 draft's wire shapes.
func TestR3Document(t *testing.T) {
	raw := []byte(`{"vocabulary":"urn:aauth:vocabulary:mcp","operations":[{"tool":"send_email"}],"parameters":{"to":"mom@example.com"},"result":{"records":214},"display":{"summary":"Send an email as you","detail":"## Action"},"unknown":1}`)
	d, err := ParseR3Document(raw, S256(raw))
	if err != nil {
		t.Fatal(err)
	}
	if d.Display.Summary != "Send an email as you" || d.Display.Detail != "## Action" || string(d.Parameters) != `{"to":"mom@example.com"}` || string(d.Result) != `{"records":214}` {
		t.Fatalf("%+v", d)
	}
	if _, err := ParseR3Document(raw, S256([]byte("other"))); err == nil {
		t.Error("a hash mismatch passed")
	}
	bad := []byte(`{"operations":[]}`)
	if _, err := ParseR3Document(bad, S256(bad)); err == nil {
		t.Error("no vocabulary passed")
	}
	notJSON := []byte(`x`)
	if _, err := ParseR3Document(notJSON, S256(notJSON)); err == nil {
		t.Error("not JSON passed")
	}
	// Operations compare by JSON value and under one vocabulary.
	doc := &R3Document{Vocabulary: "v", Operations: []json.RawMessage{json.RawMessage(`{"a":1,"b":2}`)}}
	if !doc.CoversOperations(&R3Operations{Vocabulary: "v", Operations: []json.RawMessage{json.RawMessage(`{ "b":2, "a":1 }`)}}) {
		t.Error("member order mattered")
	}
	if doc.CoversOperations(&R3Operations{Vocabulary: "w", Operations: []json.RawMessage{json.RawMessage(`{"a":1,"b":2}`)}}) {
		t.Error("another vocabulary passed")
	}
	if doc.CoversOperations(&R3Operations{Vocabulary: "v", Operations: []json.RawMessage{json.RawMessage(`{"a":1}`)}}) {
		t.Error("another operation passed")
	}
}

// The Budgets draft's wire shapes.
func TestBudgetShapes(t *testing.T) {
	if err := (Budget{Amount: 5000000, Unit: "USD", Decimals: 6}).Validate(); err != nil {
		t.Error(err)
	}
	if err := (Budget{Amount: MaxBudgetAmount, Unit: "USD"}).Validate(); err != nil {
		t.Error(err)
	}
	for _, b := range []Budget{
		{Amount: -1, Unit: "USD"},
		{Amount: MaxBudgetAmount + 1, Unit: "USD"},
		{Amount: 1},
		{Amount: 1, Unit: "USD", Decimals: -1},
	} {
		if err := b.Validate(); err == nil {
			t.Errorf("%+v passed", b)
		}
	}
	if b := jsonBytes(t, Budget{Amount: 5000000, Unit: "USD", Decimals: 6}); string(b) != `{"amount":5000000,"unit":"USD","decimals":6}` {
		t.Errorf("%s", b)
	}
	if b := jsonBytes(t, BudgetConsumed{Jti: "at-71b9d0", Consumed: 2000000}); string(b) != `{"jti":"at-71b9d0","consumed":2000000}` {
		t.Errorf("%s", b)
	}
	// "the amount as a human-readable figure in its unit: $5.00, not {5000000, USD, 6}".
	for _, c := range []struct {
		b    Budget
		want string
	}{
		{Budget{5000000, "USD", 6}, "$5.00"},
		{Budget{4200, "USD", 2}, "$42.00"},
		{Budget{1234567, "USD", 6}, "$1.234567"},
		{Budget{1500, "EUR", 2}, "€15.00"},
		{Budget{5, "GBP", 0}, "£5.00"},
		{Budget{5000, "tokens", 0}, "5000 tokens"},
		{Budget{1500, "credits", 2}, "15 credits"},
		{Budget{1550, "credits", 2}, "15.5 credits"},
	} {
		if got := FormatBudgetAmount(c.b); got != c.want {
			t.Errorf("%+v: %s, want %s", c.b, got, c.want)
		}
	}
	// Usage requests.
	if err := (&UsageRequest{Sub: "s"}).Validate(); err != nil {
		t.Error(err)
	}
	if err := (&UsageRequest{JKTs: []string{"j"}}).Validate(); err != nil {
		t.Error(err)
	}
	wantInvalidRequest(t, (&UsageRequest{}).Validate())
	wantInvalidRequest(t, (&UsageRequest{Sub: "s", Tenant: "t"}).Validate())
	var u UsageResponse
	if err := json.Unmarshal([]byte(`{"as_of":1754619970,"aud":"https://ps.example","unit":"USD","decimals":6,"sub":"8f","usage":{"day":1243180,"all_time":61438050},"jkts":{"Nz":38215600}}`), &u); err != nil {
		t.Fatal(err)
	}
	if u.Usage.AllTime != 61438050 || *u.Usage.Day != 1243180 || u.Usage.Week != nil || u.JKTs["Nz"] != 38215600 {
		t.Errorf("%+v", u)
	}
}

// Metadata member names, against the protocol's PS example.
func TestPersonServerMetadataMembers(t *testing.T) {
	const doc = `{
		"issuer":"https://ps.example","name":"Example Person Server","description":"d",
		"logo_uri":"https://ps.example/logo.png","logo_dark_uri":"https://ps.example/logo-dark.png",
		"documentation_uri":"https://ps.example/docs","tos_uri":"https://ps.example/tos","policy_uri":"https://ps.example/privacy",
		"auth_token_endpoint":"https://ps.example/token","person_token_endpoint":"https://ps.example/person",
		"mission_endpoint":"https://ps.example/mission","permission_endpoint":"https://ps.example/permission",
		"audit_endpoint":"https://ps.example/audit","interaction_endpoint":"https://ps.example/interaction",
		"mission_control_endpoint":"https://ps.example/mission-control","jwks_uri":"https://ps.example/.well-known/jwks.json",
		"revocation_endpoint":"https://ps.example/revoke","scopes_supported":["openid"],"claims_supported":["sub"],
		"accept_signature_algs":["Ed25519"]}`
	var m PersonServerMetadata
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatal(err)
	}
	if err := m.Validate(IdentifierPolicy{}); err != nil {
		t.Fatal(err)
	}
	var out, in map[string]any
	_ = json.Unmarshal(jsonBytes(t, m), &out)
	_ = json.Unmarshal([]byte(doc), &in)
	for k := range in {
		if _, ok := out[k]; !ok {
			t.Errorf("member %s lost", k)
		}
	}
	mutate := func(f func(*PersonServerMetadata)) error {
		c := m
		f(&c)
		return c.Validate(IdentifierPolicy{})
	}
	for name, f := range map[string]func(*PersonServerMetadata){
		"bad issuer":         func(c *PersonServerMetadata) { c.Issuer = "https://ps.example/" },
		"no jwks_uri":        func(c *PersonServerMetadata) { c.JWKSURI = "" },
		"http jwks_uri":      func(c *PersonServerMetadata) { c.JWKSURI = "http://ps.example/j" },
		"no auth endpoint":   func(c *PersonServerMetadata) { c.AuthTokenEndpoint = "" },
		"no person endpoint": func(c *PersonServerMetadata) { c.PersonTokenEndpoint = "" },
		"endpoint query":     func(c *PersonServerMetadata) { c.AuthTokenEndpoint = "https://ps.example/token?x=1" },
		"http logo":          func(c *PersonServerMetadata) { c.LogoURI = "http://ps.example/l" },
		"http revocation":    func(c *PersonServerMetadata) { c.RevocationEndpoint = "http://ps.example/r" },
		"lists EdDSA":        func(c *PersonServerMetadata) { c.AcceptSignatureAlgs = []string{"EdDSA"} },
		"lists none":         func(c *PersonServerMetadata) { c.AcceptSignatureAlgs = []string{"none"} },
		"lists HS256":        func(c *PersonServerMetadata) { c.AcceptSignatureAlgs = []string{"HS256"} },
	} {
		if err := mutate(f); err == nil {
			t.Errorf("%s: passed", name)
		}
	}
	// "The four REQUIRED fields ... are the whole of what a conformant PS publishes."
	floor := PersonServerMetadata{CommonMetadata: CommonMetadata{Issuer: testPS, JWKSURI: testPS + "/j"}, AuthTokenEndpoint: testPS + "/token", PersonTokenEndpoint: testPS + "/person"}
	if err := floor.Validate(IdentifierPolicy{}); err != nil {
		t.Error(err)
	}
}

func TestOtherMetadataValidation(t *testing.T) {
	p := IdentifierPolicy{}
	as := AccessServerMetadata{CommonMetadata: CommonMetadata{Issuer: testAS, JWKSURI: testAS + "/j"}, AuthTokenEndpoint: testAS + "/token"}
	if err := as.Validate(p); err != nil {
		t.Error(err)
	}
	as.AuthTokenEndpoint = ""
	if as.Validate(p) == nil {
		t.Error("AS with no auth_token_endpoint")
	}
	ap := AgentProviderMetadata{CommonMetadata: CommonMetadata{Issuer: testAP, JWKSURI: testAP + "/j"}, CallbackEndpoint: testAP + "/cb", LocalhostCallbackAllowed: true}
	if err := ap.Validate(p); err != nil {
		t.Error(err)
	}
	ap.JWKSURI = ""
	if ap.Validate(p) == nil {
		t.Error("AP with no jwks_uri")
	}
	// "A resource that only verifies agent signatures ... MAY omit it".
	res := ResourceMetadata{CommonMetadata: CommonMetadata{Issuer: testResource}, AccessMode: AccessModePerCall, SignatureWindow: 120, AdditionalSignatureComponents: []string{"content-type"}}
	if err := res.Validate(p); err != nil {
		t.Error(err)
	}
	res.UsageEndpoint = "http://resource.example/usage"
	if res.Validate(p) == nil {
		t.Error("http usage_endpoint")
	}
	ss := SupervisorMetadata{CommonMetadata: CommonMetadata{Issuer: testSS}, ConnectEndpoint: testSS + "/connect", MissionEndpoint: testSS + "/mission", DecisionEndpoint: testSS + "/decision", EventEndpoint: testSS + "/event"}
	if err := ss.Validate(p); err != nil {
		t.Error(err)
	}
	ss.EventEndpoint = ""
	if ss.Validate(p) == nil {
		t.Error("SS with no event_endpoint")
	}
}

// The supervision draft's examples, member by member.
func TestSupervisionEnvelopeWire(t *testing.T) {
	const mission = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	var q DecisionRequest
	if err := json.Unmarshal([]byte(`{
		"sdi":"d-4d2a92","act":"auth_token","iat":1790600200,"sub":"8f14e45fceea167a5a36dedd4bea2543",
		"agent":"aauth:assistant@agent.example","mission_s256":"`+mission+`","agent_token":"eyJa",
		"resource_token":"eyJr","presented_token":"eyJp","justification":"I need to read your calendar.","future":1}`), &q); err != nil {
		t.Fatal(err)
	}
	if err := q.Validate(); err != nil {
		t.Fatal(err)
	}
	if q.SDI != "d-4d2a92" || q.Act != ActAuthToken || q.Sub == "" || q.Agent != "aauth:assistant@agent.example" || q.MissionS256 != mission || q.ResourceToken != "eyJr" || q.PresentedToken != "eyJp" {
		t.Fatalf("%+v", q)
	}
	// The additive extension members marshal flat, and are absent when unused.
	b := jsonBytes(t, DecisionRequest{Act: ActPersonToken, Envelope: Envelope{SDI: "d", Iat: 1, Sub: "s", Agent: "a", Capabilities: []string{"clarification"}}, AgentToken: "t", Resource: testResource})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["sdi"] != "d" || m["act"] != "person_token" || m["resource"] != testResource || m["capabilities"] == nil {
		t.Errorf("%s", b)
	}
	if _, ok := m["id"]; ok {
		t.Errorf("empty id on the wire: %s", b)
	}
	if _, ok := m["r3"]; ok {
		t.Errorf("empty r3 on the wire: %s", b)
	}
	r3 := jsonBytes(t, DecisionRequest{Act: ActAuthToken, R3: &R3Forwarded{URI: "https://r.example/r3", S256: "x", Result: json.RawMessage(`{"records":214}`)}})
	if !strings.Contains(string(r3), `"r3":{"uri":"https://r.example/r3","s256":"x","result":{"records":214}}`) {
		t.Errorf("%s", r3)
	}
	// Every act's required members, and an unrecognized act.
	env := Envelope{SDI: "d", Iat: 1, Sub: "s", Agent: "a"}
	for name, c := range map[string]DecisionRequest{
		"no act":               {Envelope: env},
		"bad act":              {Envelope: env, Act: "other"},
		"person no resource":   {Envelope: env, Act: ActPersonToken, AgentToken: "t"},
		"person no token":      {Envelope: env, Act: ActPersonToken, Resource: testResource},
		"auth no presented":    {Envelope: env, Act: ActAuthToken, AgentToken: "t", ResourceToken: "r"},
		"permission no action": {Envelope: env, Act: ActPermission},
		"no sdi":               {Envelope: Envelope{Iat: 1, Sub: "s", Agent: "a"}, Act: ActPermission, Action: "x"},
		"no iat":               {Envelope: Envelope{SDI: "d", Sub: "s", Agent: "a"}, Act: ActPermission, Action: "x"},
		"no sub":               {Envelope: Envelope{SDI: "d", Iat: 1, Agent: "a"}, Act: ActPermission, Action: "x"},
		"no agent":             {Envelope: Envelope{SDI: "d", Iat: 1, Sub: "s"}, Act: ActPermission, Action: "x"},
		"bad mission":          {Envelope: Envelope{SDI: "d", Iat: 1, Sub: "s", Agent: "a", MissionS256: "x"}, Act: ActPermission, Action: "x"},
	} {
		c := c
		if err := c.Validate(); err == nil {
			t.Errorf("%s: passed", name)
		} else {
			wantInvalidRequest(t, err)
		}
	}
	for _, c := range []DecisionRequest{
		{Envelope: env, Act: ActPersonToken, AgentToken: "t", Resource: testResource},
		{Envelope: env, Act: ActPermission, Action: "SendEmail", Parameters: json.RawMessage(`{}`)},
	} {
		c := c
		if err := c.Validate(); err != nil {
			t.Error(err)
		}
	}
}

func TestConnectAndMissionSupervision(t *testing.T) {
	if err := (&ConnectRequest{Action: ConnectActionConnect, ID: "c-8f3k2n9p", Iat: 1790600000, Sub: "s", Tenant: "acme.example"}).Validate(); err != nil {
		t.Error(err)
	}
	for _, q := range []ConnectRequest{
		{}, {Action: "other", ID: "i", Iat: 1, Sub: "s"}, {Action: ConnectActionDisconnect, Iat: 1, Sub: "s"},
		{Action: ConnectActionConnect, ID: "i", Sub: "s"}, {Action: ConnectActionConnect, ID: "i", Iat: 1},
	} {
		q := q
		wantInvalidRequest(t, q.Validate())
	}
	env := Envelope{SDI: "d", Iat: 1, Sub: "s", Agent: "a"}
	report := Envelope{ID: "e", SDI: "d", Iat: 1, Sub: "s", Agent: "a"}
	if err := (&MissionProposalSupervision{Envelope: env, AgentToken: "t", Description: "d"}).Validate(); err != nil {
		t.Error(err)
	}
	wantInvalidRequest(t, (&MissionProposalSupervision{Envelope: env, Description: "d"}).Validate())
	wantInvalidRequest(t, (&MissionProposalSupervision{Envelope: env, AgentToken: "t"}).Validate())
	wantInvalidRequest(t, (&MissionProposalSupervision{AgentToken: "t", Description: "d"}).Validate())

	for name, q := range map[string]MissionActionSupervision{
		"update":     {Action: MissionActionUpdate, Envelope: env, Description: "d"},
		"completion": {Action: MissionActionCompletion, Envelope: env, Summary: "s"},
		"approved":   {Action: MissionActionApproved, Envelope: report, S256: "x", Mission: "y"},
		"terminated": {Action: MissionActionTerminated, Envelope: report, Reason: TerminationCompleted},
	} {
		q := q
		if err := q.Validate(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, q := range map[string]MissionActionSupervision{
		"no action":            {Envelope: env},
		"unknown action":       {Action: "x", Envelope: env},
		"update no text":       {Action: MissionActionUpdate, Envelope: env},
		"update no sdi":        {Action: MissionActionUpdate, Envelope: Envelope{Iat: 1, Sub: "s", Agent: "a"}, Description: "d"},
		"completion no text":   {Action: MissionActionCompletion, Envelope: env},
		"approved no id":       {Action: MissionActionApproved, Envelope: env, S256: "x", Mission: "y"},
		"approved no blob":     {Action: MissionActionApproved, Envelope: report},
		"terminated no reason": {Action: MissionActionTerminated, Envelope: report},
	} {
		q := q
		if err := q.Validate(); err == nil {
			t.Errorf("%s: passed", name)
		}
	}
	if (&MissionActionSupervision{Action: MissionActionUpdate}).IsReport() || !(&MissionActionSupervision{Action: MissionActionApproved}).IsReport() || !(&MissionActionSupervision{Action: MissionActionTerminated}).IsReport() {
		t.Error("IsReport")
	}
}

// supervision, Supervision Decisions: the ask example, the narrowed allow, and the mission allow.
func TestDecisions(t *testing.T) {
	d, err := ParseDecision([]byte(`{"sdi":"d-4d2a91","decision":"ask","summary":"The agent wants to book a $3,200 flight.","recommendation":"deny",
		"apply_options":[{"value":"once","label":"This booking only"},{"value":"mission","label":"Anything else this agent asks on this trip"}]}`), "d-4d2a91")
	if err != nil {
		t.Fatal(err)
	}
	if d.Decision != DecisionAsk || d.Recommendation != "deny" || len(d.ApplyOptions) != 2 || d.ApplyOptions[0].Value != ApplyOnce || d.ApplyOptions[1].Value != ApplyMission {
		t.Fatalf("%+v", d)
	}
	d, err = ParseDecision([]byte(`{"sdi":"d","decision":"allow","scope":"calendar.read","exp":1790600000,"budget":{"amount":5,"unit":"USD","decimals":2}}`), "d")
	if err != nil || d.Scope != "calendar.read" || d.Exp != 1790600000 || d.Budget.Amount != 5 {
		t.Fatalf("%+v %v", d, err)
	}
	d, err = ParseDecision([]byte(`{"sdi":"d-7a1c02","decision":"allow","description":"Plan","approved_tools":[{"name":"WebSearch","description":"Search the web"}],"approved_resources":["https://flights.example"],"expires_at":"2026-11-01T00:00:00Z"}`), "d-7a1c02")
	if err != nil || d.Description != "Plan" || d.ApprovedTools[0].Name != "WebSearch" || d.ApprovedResources[0] != "https://flights.example" || d.ExpiresAt == "" {
		t.Fatalf("%+v %v", d, err)
	}
	d, err = ParseDecision([]byte(`{"sdi":"d","decision":"ask","proposed":{"description":"refined","approved_tools":[]}}`), "d")
	if err != nil || d.Proposed == nil || d.Proposed.Description != "refined" {
		t.Fatalf("%+v %v", d, err)
	}
	d, err = ParseDecision([]byte(`{"sdi":"d","decision":"deny","reason":"**no**","future":{"a":1}}`), "d")
	if err != nil || d.Reason != "**no**" {
		t.Fatalf("%+v %v", d, err)
	}
	// "The response MUST echo the request's sdi", and a malformed body is the SS being unavailable.
	for name, tc := range map[string][2]string{
		"sdi differs":        {`{"sdi":"other","decision":"allow"}`, "d"},
		"no sdi":             {`{"decision":"allow"}`, "d"},
		"not JSON":           {`x`, "d"},
		"unknown decision":   {`{"sdi":"d","decision":"maybe"}`, "d"},
		"no decision":        {`{"sdi":"d"}`, "d"},
		"clarify in a 200":   {`{"sdi":"d","decision":"clarify"}`, "d"},
		"bad recommendation": {`{"sdi":"d","decision":"ask","recommendation":"perhaps"}`, "d"},
	} {
		if _, err := ParseDecision([]byte(tc[0]), tc[1]); !errors.Is(err, ErrMalformedDecision) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// A decision marshals with the members flat, as the draft's examples show.
	b := jsonBytes(t, Decision{SDI: "d", Decision: DecisionAllow, MissionRefinement: MissionRefinement{Description: "x"}})
	if string(b) != `{"sdi":"d","decision":"allow","description":"x"}` {
		t.Errorf("%s", b)
	}
	// The clarify body, reading like the others.
	c := jsonBytes(t, ClarifyBody{SDI: "d-4d2a92", Status: PendingStatusPending, Decision: DecisionClarify, Clarification: "Is the fare refundable?", Timeout: 120})
	if string(c) != `{"sdi":"d-4d2a92","status":"pending","decision":"clarify","clarification":"Is the fare refundable?","timeout":120}` {
		t.Errorf("%s", c)
	}
}

// supervision, Event Endpoint: all seven events.
func TestParseEvent(t *testing.T) {
	env := `"id":"e-1","iat":1790600130,"sub":"s","agent":"aauth:a@agent.example"`
	for name, tc := range map[string]struct {
		body  string
		check func(any) bool
	}{
		EventPersonTokenIssued: {`{"event":"person_token.issued","sdi":"d",` + env + `,"jti":"j","aud":"https://r.example","exp":5}`, func(e any) bool {
			v, ok := e.(*PersonTokenIssuedEvent)
			return ok && v.Jti == "j" && v.Aud == "https://r.example" && v.Exp == 5 && v.SDI == "d"
		}},
		EventAuthTokenIssued: {`{"event":"auth_token.issued","sdi":"d",` + env + `,"jti":"j","aud":"https://r.example","scope":"a","exp":5,"issuer":"https://as.example"}`, func(e any) bool {
			v, ok := e.(*AuthTokenIssuedEvent)
			return ok && v.Scope == "a" && v.Issuer == "https://as.example"
		}},
		EventDecisionResolved: {`{"event":"decision.resolved","sdi":"d",` + env + `,"answer":"allow","apply":"mission","recommendation":"deny","supervisor":"unavailable","reason":"r","transcript":[{"from":"person","text":"Refundable?"},{"from":"agent","text":"Yes."}],"proposed":{"description":"x"}}`, func(e any) bool {
			v, ok := e.(*DecisionResolvedEvent)
			return ok && v.Answer == AnswerAllow && v.Apply == ApplyMission && v.Supervisor == SupervisorUnavailable && len(v.Transcript) == 2 && v.Transcript[0].From == TranscriptFromPerson && v.Proposed.Description == "x"
		}},
		EventAuditRecorded: {`{"event":"audit.recorded",` + env + `,"action":"WebSearch","description":"d","parameters":{"q":"x"},"result":{"status":"completed"}}`, func(e any) bool {
			v, ok := e.(*AuditRecordedEvent)
			return ok && v.Action == "WebSearch" && string(v.Parameters) == `{"q":"x"}`
		}},
		EventInteractionRequested: {`{"event":"interaction.requested",` + env + `,"type":"interaction","url":"https://b.example/confirm"}`, func(e any) bool {
			v, ok := e.(*InteractionRequestedEvent)
			return ok && v.Type == "interaction" && v.URL == "https://b.example/confirm"
		}},
		EventAgentBound: {`{"event":"agent.bound",` + env + `,"agent_iss":"https://agent.example","agent_sub":"aauth:a@agent.example"}`, func(e any) bool {
			v, ok := e.(*AgentBoundEvent)
			return ok && v.AgentIss == "https://agent.example"
		}},
		EventRevocationReceived: {`{"event":"revocation.received",` + env + `,"token_type":"aa-agent+jwt","iss":"https://agent.example","jti":"j","exp":5}`, func(e any) bool {
			v, ok := e.(*RevocationReceivedEvent)
			return ok && v.TokenType == TypAgent && v.Iss == "https://agent.example"
		}},
	} {
		t.Run(name, func(t *testing.T) {
			e, err := ParseEvent([]byte(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if !tc.check(e) {
				t.Fatalf("%+v", e)
			}
		})
	}
	for name, body := range map[string]string{
		"unknown event":   `{"event":"other",` + env + `}`,
		"no event":        `{` + env + `}`,
		"not JSON":        `x`,
		"no id":           `{"event":"agent.bound","iat":1,"sub":"s","agent":"a"}`,
		"no sub":          `{"event":"agent.bound","id":"e","iat":1,"agent":"a"}`,
		"bad member":      `{"event":"agent.bound",` + env + `,"agent_iss":5}`,
		"resolved no sdi": `{"event":"decision.resolved",` + env + `,"answer":"allow"}`,
		"bad answer":      `{"event":"decision.resolved","sdi":"d",` + env + `,"answer":"maybe"}`,
	} {
		_, err := ParseEvent([]byte(body))
		if err == nil {
			t.Errorf("%s: parsed", name)
			continue
		}
		wantInvalidRequest(t, err)
	}
	// "The code of a relayed interaction is not forwarded."
	b := jsonBytes(t, InteractionRequestedEvent{Event: EventInteractionRequested, Type: "interaction", URL: "https://b.example/c"})
	if strings.Contains(string(b), `"code"`) {
		t.Errorf("%s", b)
	}
}

func TestISOTime(t *testing.T) {
	ts := time.Date(2026, 4, 7, 14, 30, 0, 123, time.FixedZone("x", 3600))
	if got := FormatISOTime(ts); got != "2026-04-07T13:30:00Z" {
		t.Errorf("%s", got)
	}
	if p, err := ParseISOTime("2026-04-07T14:30:00+01:00"); err != nil || !p.Equal(time.Date(2026, 4, 7, 13, 30, 0, 0, time.UTC)) {
		t.Errorf("%s %v", p, err)
	}
	if _, err := ParseISOTime("yesterday"); err == nil {
		t.Error("nonsense parsed")
	}
}

// HIGH: budget.decimals is an exponent other parties raise, and "A non-negative integer" names no top. Past 18 it isn't a scale anyone computes with, and at 64 an int64 wraps to 0 and divides by it.
func TestBudgetDecimalsAndUnitAreBounded(t *testing.T) {
	for _, b := range []Budget{
		{Amount: 1, Unit: "USD", Decimals: 19},
		{Amount: 1, Unit: "USD", Decimals: 64},
		{Amount: 1, Unit: "USD", Decimals: 9_000_000_000_000_000_000},
		{Amount: 1, Unit: "USD", Decimals: -1},
		{Amount: 1, Unit: "free trial, no charge", Decimals: 0},
		{Amount: 1, Unit: "USD\n", Decimals: 0},
		{Amount: 1, Unit: "", Decimals: 0},
	} {
		if err := b.Validate(); err == nil {
			t.Errorf("%+v passed", b)
		}
	}
	for _, b := range []Budget{{Amount: 1, Unit: "USD", Decimals: 18}, {Amount: MaxBudgetAmount, Unit: "tokens", Decimals: 0}, {Amount: 5, Unit: "x402.usdc", Decimals: 6}} {
		if err := b.Validate(); err != nil {
			t.Errorf("%+v: %v", b, err)
		}
	}
	// A figure that came from somewhere else and was never validated is shown, with nothing worked out. It doesn't panic, and it doesn't run for ever.
	done := make(chan string, 1)
	go func() {
		done <- FormatBudgetAmount(Budget{Amount: 7, Unit: "USD", Decimals: 9_000_000_000_000_000_000})
	}()
	select {
	case got := <-done:
		if got != "7 USD" {
			t.Errorf("format = %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("FormatBudgetAmount ran on past a hostile scale")
	}
	for _, d := range []int{64, 19, -1} {
		if got := FormatBudgetAmount(Budget{Amount: 7, Unit: "USD", Decimals: d}); got != "7 USD" {
			t.Errorf("decimals %d: %q", d, got)
		}
	}
	if got := FormatBudgetAmount(Budget{Amount: 1_500_000_000_000_000_000 / 1_000, Unit: "ETH", Decimals: 18}); got == "" {
		t.Error("the largest scale gave nothing")
	}
}

// B-20: "MUST validate all values before processing". A relayed code goes in a URL the person is sent to.
func TestRelayedCodeIsValidated(t *testing.T) {
	p := IdentifierPolicy{}
	bad := InteractionRequest{Type: InteractionTypeInteraction, URL: "https://r.example/i", Code: "A&callback=https://evil.example"}
	if err := bad.Validate(p); err == nil {
		t.Error("a code with a query in it passed")
	}
	good := InteractionRequest{Type: InteractionTypeInteraction, URL: "https://r.example/i", Code: "X7K2-M9P4"}
	if err := good.Validate(p); err != nil {
		t.Error(err)
	}
	// "url (OPTIONAL)".
	if err := (&InteractionRequest{Type: InteractionTypePayment, Description: "Approve it"}).Validate(p); err != nil {
		t.Error(err)
	}
	// A question names no address, and one that isn't an address goes nowhere near the SS.
	if err := (&InteractionRequest{Type: InteractionTypeQuestion, Question: "x", URL: "<<<"}).Validate(p); err == nil {
		t.Error("a question with a bad url passed")
	}
}
