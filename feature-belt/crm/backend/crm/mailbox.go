package crm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"mime"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"scaffoldapp/backend/internal/app"
)

// Mailboxes: one person's connected Gmail or Outlook account, read and sent
// from live with their own token. Nothing is copied from a mailbox except an
// email the person sends from the CRM or chooses to log.

// Mail capabilities (connectors: app.Connections).
const (
	CapGoogleMailRead     = "crm.google.mail_read"
	CapGoogleMailSend     = "crm.google.mail_send"
	CapMicrosoftMailRead  = "crm.microsoft.mail_read"
	CapMicrosoftMailSend  = "crm.microsoft.mail_send"
	mailProviderGoogle    = "google"
	mailProviderMicrosoft = "microsoft"
)

var mailCaps = []app.Capability{
	{Key: CapGoogleMailRead, Module: "crm", Provider: mailProviderGoogle, Label: "Recent Gmail with a contact in the CRM (to log)",
		Scopes: []string{"https://www.googleapis.com/auth/gmail.readonly"}},
	{Key: CapGoogleMailSend, Module: "crm", Provider: mailProviderGoogle, Label: "Send email from the CRM with Gmail", Write: true,
		Scopes: []string{"https://www.googleapis.com/auth/gmail.send"}},
	{Key: CapMicrosoftMailRead, Module: "crm", Provider: mailProviderMicrosoft, Label: "Recent Outlook mail with a contact in the CRM (to log)",
		Scopes: []string{"Mail.Read"}},
	{Key: CapMicrosoftMailSend, Module: "crm", Provider: mailProviderMicrosoft, Label: "Send email from the CRM with Outlook", Write: true,
		Scopes: []string{"Mail.Send", "Mail.ReadWrite"}},
}

func mailCap(provider string, send bool) string {
	switch {
	case provider == mailProviderGoogle && send:
		return CapGoogleMailSend
	case provider == mailProviderGoogle:
		return CapGoogleMailRead
	case send:
		return CapMicrosoftMailSend
	default:
		return CapMicrosoftMailRead
	}
}

// outMail is a plain-text email to send.
type outMail struct {
	To, CC  []string
	Subject string
	Body    string
}

// MailSummary is one message in a mailbox, as the person's list shows it.
type MailSummary struct {
	MessageID string    `json:"message_id"`
	ThreadID  string    `json:"thread_id"`
	Subject   string    `json:"subject"`
	Snippet   string    `json:"snippet"`
	From      string    `json:"from"`
	To        []string  `json:"to"`
	At        time.Time `json:"at"`
	Inbound   bool      `json:"inbound"` // from someone else to the person
	Logged    bool      `json:"logged"`  // already in the CRM
}

// mailMessage is one message read in full (to log it).
type mailMessage struct {
	MailSummary
	CC   []string
	Body string
}

// mailbox is one connected account.
type mailbox interface {
	label() string
	send(ctx context.Context, m outMail) (messageID, threadID string, err error)
	recent(ctx context.Context, emails []string, limit int) ([]MailSummary, error)
	get(ctx context.Context, messageID string) (*mailMessage, error)
}

// ---- Gmail (REST v1) ------------------------------------------------------------

type gmailBox struct {
	hc      *http.Client
	base    string // …/gmail/v1
	account string
}

// gmailBase is the Gmail API base for a Google connection: its "gmail"
// endpoint when the connections module lists one, else derived from the
// Drive endpoint's host (so a development override moves it too).
func gmailBase(c app.Connection) string {
	if b := c.Endpoints["gmail"]; b != "" {
		return strings.TrimRight(b, "/")
	}
	if d := c.Endpoints["drive"]; d != "" && strings.Contains(d, "www.googleapis.com/drive/v3") {
		return strings.Replace(strings.TrimRight(d, "/"), "www.googleapis.com/drive/v3", "gmail.googleapis.com/gmail/v1", 1)
	}
	return "https://gmail.googleapis.com/gmail/v1"
}

func googleParse(_ int, body []byte) (string, map[string]string) {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	return e.Error.Message, nil
}

func (g *gmailBox) label() string { return "Gmail" }

func (g *gmailBox) call(ctx context.Context, method, path string, q url.Values, body, out any) error {
	return xreq{hc: g.hc, label: "Gmail", method: method, url: g.base + path, query: q, body: body, parse: googleParse}.do(ctx, out)
}

func (g *gmailBox) send(ctx context.Context, m outMail) (string, string, error) {
	raw, err := rfc822(g.account, m)
	if err != nil {
		return "", "", err
	}
	var res struct {
		ID       string `json:"id"`
		ThreadID string `json:"threadId"`
	}
	err = g.call(ctx, http.MethodPost, "/users/me/messages/send", nil, map[string]string{"raw": base64.RawURLEncoding.EncodeToString(raw)}, &res)
	return res.ID, res.ThreadID, err
}

type gmailMsg struct {
	ID           string `json:"id"`
	ThreadID     string `json:"threadId"`
	Snippet      string `json:"snippet"`
	InternalDate string `json:"internalDate"`
	Payload      struct {
		gmailPart
		Headers []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"headers"`
	} `json:"payload"`
}

type gmailPart struct {
	MimeType string `json:"mimeType"`
	Body     struct {
		Data string `json:"data"`
	} `json:"body"`
	Parts []gmailPart `json:"parts"`
}

func (m *gmailMsg) header(name string) string {
	for _, h := range m.Payload.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

func (g *gmailBox) summary(m *gmailMsg) MailSummary {
	s := MailSummary{MessageID: m.ID, ThreadID: m.ThreadID, Subject: decodeHeader(m.header("Subject")), Snippet: html.UnescapeString(m.Snippet),
		From: addrOf(m.header("From")), To: addrList(m.header("To"))}
	if ms, err := strconv.ParseInt(m.InternalDate, 10, 64); err == nil {
		s.At = time.UnixMilli(ms).UTC()
	}
	s.Inbound = !strings.EqualFold(s.From, g.account)
	return s
}

func (g *gmailBox) recent(ctx context.Context, emails []string, limit int) ([]MailSummary, error) {
	terms := make([]string, len(emails))
	for i, e := range emails {
		terms[i] = e
	}
	who := strings.Join(terms, " OR ")
	q := url.Values{"q": {"from:(" + who + ") OR to:(" + who + ") OR cc:(" + who + ")"}, "maxResults": {strconv.Itoa(limit)}}
	var list struct {
		Messages []struct {
			ID string `json:"id"`
		} `json:"messages"`
	}
	if err := g.call(ctx, http.MethodGet, "/users/me/messages", q, nil, &list); err != nil {
		return nil, err
	}
	out := []MailSummary{}
	meta := url.Values{"format": {"metadata"}, "metadataHeaders": {"From", "To", "Cc", "Subject", "Date"}}
	for _, item := range list.Messages {
		var m gmailMsg
		if err := g.call(ctx, http.MethodGet, "/users/me/messages/"+url.PathEscape(item.ID), meta, nil, &m); err != nil {
			return nil, err
		}
		out = append(out, g.summary(&m))
	}
	return out, nil
}

func (g *gmailBox) get(ctx context.Context, id string) (*mailMessage, error) {
	var m gmailMsg
	if err := g.call(ctx, http.MethodGet, "/users/me/messages/"+url.PathEscape(id), url.Values{"format": {"full"}}, nil, &m); err != nil {
		return nil, err
	}
	out := &mailMessage{MailSummary: g.summary(&m), CC: addrList(m.header("Cc"))}
	plain, htmlBody := gmailBodies(m.Payload.gmailPart)
	out.Body = plain
	if out.Body == "" {
		out.Body = htmlToText(htmlBody)
	}
	return out, nil
}

// gmailBodies finds the first text/plain and text/html parts.
func gmailBodies(p gmailPart) (plain, htm string) {
	decode := func(s string) string {
		b, err := base64.URLEncoding.DecodeString(s)
		if err != nil {
			b, err = base64.RawURLEncoding.DecodeString(s)
		}
		if err != nil {
			return ""
		}
		return string(b)
	}
	switch {
	case p.MimeType == "text/plain" && p.Body.Data != "":
		return decode(p.Body.Data), ""
	case p.MimeType == "text/html" && p.Body.Data != "":
		return "", decode(p.Body.Data)
	}
	for _, c := range p.Parts {
		pl, h := gmailBodies(c)
		if plain == "" {
			plain = pl
		}
		if htm == "" {
			htm = h
		}
	}
	return plain, htm
}

// ---- Microsoft Graph (v1.0) ------------------------------------------------------

type graphBox struct {
	hc      *http.Client
	base    string // …/v1.0
	account string
}

func graphParse(_ int, body []byte) (string, map[string]string) {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &e)
	return e.Error.Message, nil
}

func (g *graphBox) label() string { return "Outlook" }

// Immutable ids keep a message's id when sending moves it to Sent Items.
var graphHeader = http.Header{"Prefer": {`IdType="ImmutableId"`, `outlook.body-content-type="text"`}}

func (g *graphBox) call(ctx context.Context, method, path string, q url.Values, body, out any) error {
	return xreq{hc: g.hc, label: "Outlook", method: method, url: g.base + path, query: q, body: body, parse: graphParse, header: graphHeader}.do(ctx, out)
}

type graphAddr struct {
	EmailAddress struct {
		Address string `json:"address"`
		Name    string `json:"name,omitempty"`
	} `json:"emailAddress"`
}

func graphAddrs(list []string) []graphAddr {
	out := make([]graphAddr, len(list))
	for i, a := range list {
		out[i].EmailAddress.Address = a
	}
	return out
}

func (g *graphBox) send(ctx context.Context, m outMail) (string, string, error) {
	draft := map[string]any{
		"subject": m.Subject, "body": map[string]string{"contentType": "Text", "content": m.Body},
		"toRecipients": graphAddrs(m.To), "ccRecipients": graphAddrs(m.CC),
	}
	var created struct {
		ID             string `json:"id"`
		ConversationID string `json:"conversationId"`
	}
	if err := g.call(ctx, http.MethodPost, "/me/messages", nil, draft, &created); err != nil {
		return "", "", err
	}
	if err := g.call(ctx, http.MethodPost, "/me/messages/"+url.PathEscape(created.ID)+"/send", nil, nil, nil); err != nil {
		return "", "", err
	}
	return created.ID, created.ConversationID, nil
}

type graphMsg struct {
	ID               string      `json:"id"`
	ConversationID   string      `json:"conversationId"`
	Subject          string      `json:"subject"`
	BodyPreview      string      `json:"bodyPreview"`
	From             *graphAddr  `json:"from"`
	ToRecipients     []graphAddr `json:"toRecipients"`
	CcRecipients     []graphAddr `json:"ccRecipients"`
	ReceivedDateTime time.Time   `json:"receivedDateTime"`
	SentDateTime     *time.Time  `json:"sentDateTime"`
	Body             *struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
}

func (g *graphBox) summary(m *graphMsg) MailSummary {
	s := MailSummary{MessageID: m.ID, ThreadID: m.ConversationID, Subject: m.Subject, Snippet: m.BodyPreview, At: m.ReceivedDateTime.UTC(), To: []string{}}
	if m.SentDateTime != nil {
		s.At = m.SentDateTime.UTC()
	}
	if m.From != nil {
		s.From = strings.ToLower(m.From.EmailAddress.Address)
	}
	for _, a := range m.ToRecipients {
		s.To = append(s.To, strings.ToLower(a.EmailAddress.Address))
	}
	s.Inbound = !strings.EqualFold(s.From, g.account)
	return s
}

const graphSelect = "id,conversationId,subject,bodyPreview,from,toRecipients,ccRecipients,receivedDateTime,sentDateTime"

func (g *graphBox) recent(ctx context.Context, emails []string, limit int) ([]MailSummary, error) {
	terms := make([]string, len(emails))
	for i, e := range emails {
		terms[i] = "participants:" + e
	}
	q := url.Values{"$search": {`"` + strings.Join(terms, " OR ") + `"`}, "$top": {strconv.Itoa(limit)}, "$select": {graphSelect}}
	var res struct {
		Value []graphMsg `json:"value"`
	}
	if err := g.call(ctx, http.MethodGet, "/me/messages", q, nil, &res); err != nil {
		return nil, err
	}
	out := make([]MailSummary, 0, len(res.Value))
	for i := range res.Value {
		out = append(out, g.summary(&res.Value[i]))
	}
	return out, nil
}

func (g *graphBox) get(ctx context.Context, id string) (*mailMessage, error) {
	var m graphMsg
	if err := g.call(ctx, http.MethodGet, "/me/messages/"+url.PathEscape(id), url.Values{"$select": {graphSelect + ",body"}}, nil, &m); err != nil {
		return nil, err
	}
	out := &mailMessage{MailSummary: g.summary(&m), CC: []string{}}
	for _, a := range m.CcRecipients {
		out.CC = append(out.CC, strings.ToLower(a.EmailAddress.Address))
	}
	if m.Body != nil {
		out.Body = m.Body.Content
		if strings.EqualFold(m.Body.ContentType, "html") {
			out.Body = htmlToText(out.Body)
		}
	}
	return out, nil
}

// ---- message helpers --------------------------------------------------------------

// rfc822 builds a plain-text message. Addresses were validated; header
// values can't carry line breaks.
func rfc822(from string, m outMail) ([]byte, error) {
	clean := func(s string) string { return strings.NewReplacer("\r", " ", "\n", " ").Replace(s) }
	var b strings.Builder
	if a, err := mail.ParseAddress(from); err == nil {
		b.WriteString("From: " + a.Address + "\r\n")
	}
	b.WriteString("To: " + clean(strings.Join(m.To, ", ")) + "\r\n")
	if len(m.CC) > 0 {
		b.WriteString("Cc: " + clean(strings.Join(m.CC, ", ")) + "\r\n")
	}
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", clean(m.Subject)) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n")
	enc := base64.StdEncoding.EncodeToString([]byte(strings.ReplaceAll(m.Body, "\n", "\r\n")))
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc + "\r\n")
	return []byte(b.String()), nil
}

func decodeHeader(s string) string {
	if d, err := new(mime.WordDecoder).DecodeHeader(s); err == nil {
		return d
	}
	return s
}

func addrOf(s string) string {
	if a, err := mail.ParseAddress(decodeHeader(s)); err == nil {
		return strings.ToLower(a.Address)
	}
	return strings.ToLower(strings.TrimSpace(s))
}

func addrList(s string) []string {
	out := []string{}
	if strings.TrimSpace(s) == "" {
		return out
	}
	list, err := mail.ParseAddressList(decodeHeader(s))
	if err != nil {
		return out
	}
	for _, a := range list {
		out = append(out, strings.ToLower(a.Address))
	}
	return out
}

var (
	tagRe      = regexp.MustCompile(`(?s)<(script|style)[^>]*>.*?</(script|style)>|<[^>]+>`)
	blockRe    = regexp.MustCompile(`(?i)<(br|/p|/div|/li|/tr|/h[1-6])\s*/?>`)
	blankLines = regexp.MustCompile(`\n{3,}`)
)

// htmlToText keeps the words of an HTML body (logged mail is stored as text).
func htmlToText(h string) string {
	h = blockRe.ReplaceAllString(h, "\n")
	h = tagRe.ReplaceAllString(h, "")
	h = html.UnescapeString(h)
	lines := strings.Split(h, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.TrimSpace(blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

// validAddresses lowercases and checks a recipient list.
func validAddresses(list []string, field string) ([]string, error) {
	out := []string{}
	for _, raw := range list {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		a, err := mail.ParseAddress(raw)
		if err != nil || len(a.Address) > 254 || a.Address != strings.TrimSpace(strings.Trim(raw, "<>")) && !strings.Contains(raw, "<") {
			return nil, fmt.Errorf("%s: “%s” isn't an email address", field, clip(raw, 60))
		}
		e := strings.ToLower(a.Address)
		dup := false
		for _, x := range out {
			dup = dup || x == e
		}
		if !dup {
			out = append(out, e)
		}
	}
	return out, nil
}
