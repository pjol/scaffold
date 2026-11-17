package aauth

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/dunglas/httpsfv"
)

// Component is an RFC 9421 component identifier: a name and ordered parameters.
type Component struct {
	item httpsfv.Item
}

// Comp returns the component identifier for a field or derived component, with
// no parameters. Field names are lowercased, as RFC 9421 section 2.1 requires.
func Comp(name string) Component {
	if !strings.HasPrefix(name, "@") {
		name = strings.ToLower(name)
	}
	return Component{item: httpsfv.NewItem(name)}
}

// ReqComp returns the identifier with the `req` parameter, which on a response
// takes the value from the related request (RFC 9421 section 2.4).
func ReqComp(name string) Component {
	c := Comp(name)
	c.item.Params.Add("req", true)
	return c
}

// ParseComponent parses a serialized identifier such as `"content-digest";req`.
func ParseComponent(s string) (Component, error) {
	item, err := httpsfv.UnmarshalItem([]string{s})
	if err != nil {
		return Component{}, err
	}
	if _, ok := item.Value.(string); !ok {
		return Component{}, errors.New("component identifier is not a String")
	}
	return Component{item: item}, nil
}

// Name returns the component name without parameters.
func (c Component) Name() string {
	s, _ := c.item.Value.(string)
	return s
}

// HasReq reports whether the `req` parameter is present.
func (c Component) HasReq() bool {
	if c.item.Params == nil {
		return false
	}
	_, ok := c.item.Params.Get("req")
	return ok
}

// String returns the serialized identifier: the name in quotes, then parameters.
func (c Component) String() string {
	s, err := httpsfv.Marshal(c.item)
	if err != nil {
		return ""
	}
	return s
}

// RequestMessage is what RFC 9421 reads from a request.
type RequestMessage struct {
	Method    string
	Scheme    string // http or https; empty when not known
	Authority string // normalized by NormalizeAuthority
	Path      string // before percent-decoding
	RawQuery  string
	Header    http.Header
	Trailer   http.Header
}

// ResponseMessage is what RFC 9421 reads from a response.
type ResponseMessage struct {
	Status  int
	Header  http.Header
	Trailer http.Header
}

// NormalizeAuthority applies RFC 9110 section 4.2.3 as RFC 9421 section 2.2.3
// requires: "the hostname is normalized to lowercase, and the default port is
// omitted". With scheme unknown both :80 and :443 are treated as default, since a
// server behind a TLS-terminating proxy can't tell which it is, and neither port
// is sent explicitly in practice.
func NormalizeAuthority(host, scheme string) string {
	host = strings.ToLower(host)
	h, port, err := net.SplitHostPort(host)
	if err != nil {
		return host
	}
	switch {
	case port == "443" && (scheme == "https" || scheme == ""),
		port == "80" && (scheme == "http" || scheme == ""):
		if strings.Contains(h, ":") {
			return "[" + h + "]"
		}
		return h
	}
	return host
}

// NewRequestMessage reads the RFC 9421 view of a request, for a request received
// by a server or built by a client.
//
// On the server the path and query come from the raw request line, since a router
// may rewrite r.URL and "Path components are represented by their values before
// decoding any percent-encoded octets".
func NewRequestMessage(r *http.Request) RequestMessage {
	scheme := r.URL.Scheme
	if scheme == "" && r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if host == "" {
		host = r.URL.Host
	}
	path, rawQuery := r.URL.EscapedPath(), r.URL.RawQuery
	if strings.HasPrefix(r.RequestURI, "/") {
		path, rawQuery, _ = strings.Cut(r.RequestURI, "?")
	}
	if path == "" {
		path = "/"
	}
	header := r.Header
	if r.ContentLength > 0 && r.Header.Get("Content-Length") == "" {
		header = r.Header.Clone()
		if header == nil {
			header = http.Header{}
		}
		header.Set("Content-Length", strconv.FormatInt(r.ContentLength, 10))
	}
	return RequestMessage{
		Method:    r.Method,
		Scheme:    scheme,
		Authority: NormalizeAuthority(host, scheme),
		Path:      path,
		RawQuery:  rawQuery,
		Header:    header,
		Trailer:   r.Trailer,
	}
}

// SignatureParams is the parameterized Inner List of a signature: its covered
// components and metadata. When parsed from Signature-Input it keeps the exact
// parameter order, which the signature base depends on.
type SignatureParams struct {
	Components []Component
	HasCreated bool
	Created    int64
	HasExpires bool
	Expires    int64
	Nonce      string
	KeyID      string
	Tag        string

	list *httpsfv.InnerList
}

// NewSignatureParams builds the parameters for a new signature. The order on the
// wire is created, expires, nonce, keyid, tag, matching the spec's examples.
func NewSignatureParams(components []Component, created time.Time) SignatureParams {
	return SignatureParams{Components: components, HasCreated: true, Created: created.Unix()}
}

// innerList returns the structure to serialize.
func (p SignatureParams) innerList() httpsfv.InnerList {
	if p.list != nil {
		return *p.list
	}
	il := httpsfv.InnerList{Params: httpsfv.NewParams()}
	for _, c := range p.Components {
		il.Items = append(il.Items, c.item)
	}
	if p.HasCreated {
		il.Params.Add("created", p.Created)
	}
	if p.HasExpires {
		il.Params.Add("expires", p.Expires)
	}
	if p.Nonce != "" {
		il.Params.Add("nonce", p.Nonce)
	}
	if p.KeyID != "" {
		il.Params.Add("keyid", p.KeyID)
	}
	if p.Tag != "" {
		il.Params.Add("tag", p.Tag)
	}
	return il
}

// Serialize returns the @signature-params component value.
func (p SignatureParams) Serialize() (string, error) {
	return httpsfv.Marshal(p.innerList())
}

// SignatureInputMember is one labeled member of a Signature-Input field. Err is
// set when that member is not a well-formed signature description. A problem with
// one label doesn't stop the others being read, since a verifier chooses which
// labels it verifies.
type SignatureInputMember struct {
	Label  string
	Params SignatureParams
	Err    error
}

// ParseSignatureInput parses Signature-Input. An error means the field is not a
// Dictionary at all.
func ParseSignatureInput(fieldValues []string) ([]SignatureInputMember, error) {
	dict, err := httpsfv.UnmarshalDictionary(fieldValues)
	if err != nil {
		return nil, fmt.Errorf("parse Signature-Input: %w", err)
	}
	var out []SignatureInputMember
	for _, label := range dict.Names() {
		member, _ := dict.Get(label)
		params, err := paramsFromMember(member)
		out = append(out, SignatureInputMember{Label: label, Params: params, Err: err})
	}
	return out, nil
}

func paramsFromMember(member httpsfv.Member) (SignatureParams, error) {
	il, ok := member.(httpsfv.InnerList)
	if !ok {
		return SignatureParams{}, errors.New("member is not an Inner List")
	}
	p := SignatureParams{list: &il}
	for _, item := range il.Items {
		if _, ok := item.Value.(string); !ok {
			return SignatureParams{}, errors.New("a covered component is not a String")
		}
		p.Components = append(p.Components, Component{item: item})
	}
	if il.Params == nil {
		return p, nil
	}
	if v, ok := il.Params.Get("created"); ok {
		if n, ok := v.(int64); ok {
			p.Created, p.HasCreated = n, true
		}
	}
	if v, ok := il.Params.Get("expires"); ok {
		n, isInt := v.(int64)
		if !isInt {
			return SignatureParams{}, errors.New("expires is not an Integer")
		}
		p.Expires, p.HasExpires = n, true
	}
	if v, ok := il.Params.Get("nonce"); ok {
		p.Nonce, _ = v.(string)
	}
	if v, ok := il.Params.Get("keyid"); ok {
		p.KeyID, _ = v.(string)
	}
	if v, ok := il.Params.Get("tag"); ok {
		p.Tag, _ = v.(string)
	}
	return p, nil
}

// ParseSignatures parses a Signature field into label and signature bytes. A
// member that is not a Byte Sequence is left out, and reads as absent.
func ParseSignatures(fieldValues []string) (map[string][]byte, error) {
	dict, err := httpsfv.UnmarshalDictionary(fieldValues)
	if err != nil {
		return nil, fmt.Errorf("parse Signature: %w", err)
	}
	out := map[string][]byte{}
	for _, label := range dict.Names() {
		member, _ := dict.Get(label)
		if item, ok := member.(httpsfv.Item); ok {
			if b, ok := item.Value.([]byte); ok {
				out[label] = b
			}
		}
	}
	return out, nil
}

// sfTypes are the Structured Fields this package knows the type of, which the
// `sf` parameter requires: "If the application does not know the type of the
// field... the use of this flag will produce an error."
var sfTypes = map[string]string{
	"signature-input":         "dictionary",
	"signature":               "dictionary",
	"signature-key":           "dictionary",
	"signature-error":         "dictionary",
	"accept-signature":        "dictionary",
	"content-digest":          "dictionary",
	"repr-digest":             "dictionary",
	"want-content-digest":     "dictionary",
	"aauth-requirement":       "dictionary",
	"aauth-capabilities":      "list",
	"accept-signature-scheme": "list",
	"accept-signature-alg":    "list",
}

var obsFold = regexp.MustCompile(`\r?\n[ \t]+`)

// canonicalFieldValues strips each value and removes obsolete folding (RFC 9421
// section 2.1, steps 2 and 3).
func canonicalFieldValues(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = strings.Trim(obsFold.ReplaceAllString(v, " "), " \t")
	}
	return out
}

// SignatureBase creates the RFC 9421 signature base (section 2.5).
//
// With resp nil the target message is the request. With resp set it is the
// response, and req supplies components carrying the `req` parameter and may be
// nil if none do.
func SignatureBase(req *RequestMessage, resp *ResponseMessage, p SignatureParams) (string, error) {
	return signatureBase(req, resp, p, sfTypes)
}

func signatureBase(req *RequestMessage, resp *ResponseMessage, p SignatureParams, sf map[string]string) (string, error) {
	var b strings.Builder
	seen := map[string]bool{}
	for _, c := range p.Components {
		id := c.String()
		if id == "" {
			return "", errors.New("component identifier cannot be serialized")
		}
		if seen[id] {
			return "", fmt.Errorf("component %s is covered twice", id)
		}
		seen[id] = true
		value, err := componentValue(c, req, resp, sf)
		if err != nil {
			return "", fmt.Errorf("component %s: %w", id, err)
		}
		if strings.ContainsAny(value, "\r\n") {
			return "", fmt.Errorf("component %s has a newline in its value", id)
		}
		b.WriteString(id)
		b.WriteString(": ")
		b.WriteString(value)
		b.WriteByte('\n')
	}
	params, err := p.Serialize()
	if err != nil {
		return "", fmt.Errorf("serialize signature parameters: %w", err)
	}
	b.WriteString(`"@signature-params": `)
	b.WriteString(params)
	out := b.String()
	for i := 0; i < len(out); i++ {
		if out[i] >= 0x80 {
			return "", errors.New("signature base contains a non-ASCII character")
		}
	}
	return out, nil
}

// componentValue resolves one identifier per RFC 9421 section 2.5, step 2.5.
func componentValue(c Component, req *RequestMessage, resp *ResponseMessage, sf map[string]string) (string, error) {
	name := c.Name()
	if name == "@signature-params" {
		return "", errors.New("@signature-params must not be listed as a covered component")
	}
	hasReq, hasSF, hasBS, hasTR, hasKey, hasName := false, false, false, false, false, false
	var key, queryName string
	if c.item.Params != nil {
		for _, pn := range c.item.Params.Names() {
			v, _ := c.item.Params.Get(pn)
			switch pn {
			case "req", "sf", "bs", "tr":
				if v != true {
					return "", fmt.Errorf("parameter %s is not a Boolean true", pn)
				}
				switch pn {
				case "req":
					hasReq = true
				case "sf":
					hasSF = true
				case "bs":
					hasBS = true
				case "tr":
					hasTR = true
				}
			case "key":
				s, ok := v.(string)
				if !ok {
					return "", errors.New("parameter key is not a String")
				}
				key, hasKey = s, true
			case "name":
				s, ok := v.(string)
				if !ok {
					return "", errors.New("parameter name is not a String")
				}
				queryName, hasName = s, true
			default:
				return "", fmt.Errorf("parameter %s is not understood", pn)
			}
		}
	}
	if hasBS && (hasSF || hasKey) {
		return "", errors.New("parameter bs is incompatible with sf and key")
	}
	if hasReq && resp == nil {
		return "", errors.New("the req parameter must not be used on a request")
	}
	if hasReq && req == nil {
		return "", errors.New("the req parameter needs the related request")
	}

	if strings.HasPrefix(name, "@") {
		if hasSF || hasBS || hasTR || hasKey {
			return "", errors.New("a field parameter on a derived component")
		}
		if hasName && name != "@query-param" {
			return "", errors.New("parameter name applies only to @query-param")
		}
		if name == "@status" {
			if resp == nil {
				return "", errors.New("@status must not be used in a request message")
			}
			if hasReq {
				return "", errors.New("@status does not target the request")
			}
			if resp.Status < 100 || resp.Status > 999 {
				return "", fmt.Errorf("status %d is not three digits", resp.Status)
			}
			return strconv.Itoa(resp.Status), nil
		}
		if resp != nil && !hasReq {
			return "", errors.New("a request component on a response needs the req parameter")
		}
		if req == nil {
			return "", errors.New("no request to derive from")
		}
		return derivedValue(name, queryName, hasName, req)
	}

	if hasName {
		return "", errors.New("parameter name applies only to @query-param")
	}
	var header, trailer http.Header
	switch {
	case resp == nil || hasReq:
		if req == nil {
			return "", errors.New("no request to read from")
		}
		header, trailer = req.Header, req.Trailer
	default:
		header, trailer = resp.Header, resp.Trailer
	}
	source := header
	if hasTR {
		source = trailer
	}
	values := source.Values(name)
	if len(values) == 0 && name == "host" && !hasTR && (resp == nil || hasReq) && req != nil && req.Authority != "" {
		// net/http moves the Host header out of the header map.
		values = []string{req.Authority}
	}
	if len(values) == 0 {
		return "", errors.New("field is not present in the message")
	}
	values = canonicalFieldValues(values)
	switch {
	case hasBS:
		parts := make([]string, len(values))
		for i, v := range values {
			parts[i] = ":" + base64.StdEncoding.EncodeToString([]byte(v)) + ":"
		}
		return strings.Join(parts, ", "), nil
	case hasKey:
		dict, err := httpsfv.UnmarshalDictionary(values)
		if err != nil {
			return "", fmt.Errorf("field is not a Dictionary: %w", err)
		}
		member, ok := dict.Get(key)
		if !ok {
			return "", fmt.Errorf("Dictionary has no member %q", key)
		}
		return marshalMember(member)
	case hasSF:
		switch sf[name] {
		case "dictionary":
			dict, err := httpsfv.UnmarshalDictionary(values)
			if err != nil {
				return "", fmt.Errorf("field is not a Dictionary: %w", err)
			}
			return httpsfv.Marshal(dict)
		case "list":
			list, err := httpsfv.UnmarshalList(values)
			if err != nil {
				return "", fmt.Errorf("field is not a List: %w", err)
			}
			return httpsfv.Marshal(list)
		case "item":
			item, err := httpsfv.UnmarshalItem(values)
			if err != nil {
				return "", fmt.Errorf("field is not an Item: %w", err)
			}
			return httpsfv.Marshal(item)
		}
		return "", errors.New("the structured type of this field is not known")
	}
	return strings.Join(values, ", "), nil
}

func marshalMember(m httpsfv.Member) (string, error) {
	switch v := m.(type) {
	case httpsfv.Item:
		return httpsfv.Marshal(v)
	case httpsfv.InnerList:
		return httpsfv.Marshal(v)
	}
	return "", errors.New("unexpected Dictionary member")
}

func derivedValue(name, queryName string, hasName bool, m *RequestMessage) (string, error) {
	switch name {
	case "@method":
		return m.Method, nil
	case "@target-uri":
		scheme := m.Scheme
		if scheme == "" {
			return "", errors.New("the scheme of the request is not known")
		}
		out := scheme + "://" + m.Authority + m.Path
		if m.RawQuery != "" {
			out += "?" + m.RawQuery
		}
		return out, nil
	case "@authority":
		if m.Authority == "" {
			return "", errors.New("the authority of the request is not known")
		}
		return m.Authority, nil
	case "@scheme":
		if m.Scheme == "" {
			return "", errors.New("the scheme of the request is not known")
		}
		return strings.ToLower(m.Scheme), nil
	case "@request-target":
		if m.RawQuery != "" {
			return m.Path + "?" + m.RawQuery, nil
		}
		return m.Path, nil
	case "@path":
		if m.Path == "" {
			return "/", nil
		}
		return m.Path, nil
	case "@query":
		return "?" + m.RawQuery, nil
	case "@query-param":
		if !hasName {
			return "", errors.New("@query-param needs the name parameter")
		}
		return queryParamValue(m.RawQuery, queryName)
	}
	return "", errors.New("derived component is not understood")
}

// queryParamValue implements RFC 9421 section 2.2.8: parse as
// application/x-www-form-urlencoded, then percent-encode the name and the value.
// "If a parameter name occurs multiple times in a request, the named query
// parameter MUST NOT be included."
func queryParamValue(rawQuery, name string) (string, error) {
	found, value := false, ""
	for _, pair := range strings.Split(rawQuery, "&") {
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		dk, err := url.QueryUnescape(k)
		if err != nil {
			return "", errors.New("query parameter name is malformed")
		}
		if percentEncodeForm(dk) != name {
			continue
		}
		if found {
			return "", errors.New("query parameter occurs more than once")
		}
		dv, err := url.QueryUnescape(v)
		if err != nil {
			return "", errors.New("query parameter value is malformed")
		}
		found, value = true, percentEncodeForm(dv)
	}
	if !found {
		return "", errors.New("query parameter is not present")
	}
	return value, nil
}

// percentEncodeForm encodes with the application/x-www-form-urlencoded set of the
// URL standard, with a space as %20, as the examples in RFC 9421 section 2.2.8 do.
func percentEncodeForm(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9',
			c == '*', c == '-', c == '.', c == '_':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// SignatureInputField serializes a one-member Signature-Input field.
func SignatureInputField(label string, p SignatureParams) (string, error) {
	dict := httpsfv.NewDictionary()
	dict.Add(label, p.innerList())
	return httpsfv.Marshal(dict)
}

// SignatureField serializes a one-member Signature field.
func SignatureField(label string, signature []byte) (string, error) {
	dict := httpsfv.NewDictionary()
	dict.Add(label, httpsfv.NewItem(signature))
	return httpsfv.Marshal(dict)
}

// BuildAcceptSignature serializes an Accept-Signature field asking for one
// signature over components (RFC 9421 section 5.1).
func BuildAcceptSignature(label string, components []Component) (string, error) {
	il := httpsfv.InnerList{Params: httpsfv.NewParams()}
	for _, c := range components {
		il.Items = append(il.Items, c.item)
	}
	dict := httpsfv.NewDictionary()
	dict.Add(label, il)
	return httpsfv.Marshal(dict)
}

// AcceptSignatureRequest is one requested signature from an Accept-Signature field.
type AcceptSignatureRequest struct {
	Label      string
	Components []Component
}

// ParseAcceptSignature reads an Accept-Signature field. A member that does not
// describe a signature is skipped: "MAY ignore any signature request that does not
// fit application parameters."
func ParseAcceptSignature(fieldValues []string) ([]AcceptSignatureRequest, error) {
	dict, err := httpsfv.UnmarshalDictionary(fieldValues)
	if err != nil {
		return nil, fmt.Errorf("parse Accept-Signature: %w", err)
	}
	var out []AcceptSignatureRequest
	for _, label := range dict.Names() {
		member, _ := dict.Get(label)
		params, err := paramsFromMember(member)
		if err != nil {
			continue
		}
		out = append(out, AcceptSignatureRequest{Label: label, Components: params.Components})
	}
	return out, nil
}
