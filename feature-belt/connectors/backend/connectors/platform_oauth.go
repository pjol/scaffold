package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuth details that differ by platform: token requests (form or JSON
// bodies, client authentication, GitHub's 200-with-error answers, extra
// fields such as Salesforce's instance_url or Notion's workspace), the
// user-info call that names the account, and revocation styles that aren't
// RFC 7009.

// platformTokens is a token answer with the platform's extra fields.
type platformTokens struct {
	tokenSet
	Extra map[string]any
}

// platformTokenCall posts to p's token endpoint (base: GitLab instance).
func (s *Service) platformTokenCall(ctx context.Context, p *provider, base string, form url.Values) (*platformTokens, error) {
	endpoint := p.expand(p.TokenURL, base, "", nil)
	var body io.Reader
	contentType := "application/x-www-form-urlencoded"
	basic := p.TokenAuth == "client_secret_basic"
	if p.TokenJSON {
		m := map[string]string{}
		for k := range form {
			m[k] = form.Get(k)
		}
		b, _ := json.Marshal(m)
		body, contentType = bytes.NewReader(b), "application/json"
	} else {
		if !basic {
			form.Set("client_id", p.clientID)
			form.Set("client_secret", p.clientSecret)
		}
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", appName)
	if basic {
		req.SetBasicAuth(url.QueryEscape(p.clientID), url.QueryEscape(p.clientSecret))
	}
	resp, err := s.tokenClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mt == "application/x-www-form-urlencoded" || mt == "text/plain" {
		v, _ := url.ParseQuery(string(raw))
		m = map[string]any{}
		for k := range v {
			m[k] = v.Get(k)
		}
	} else {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		_ = dec.Decode(&m)
	}
	if resp.StatusCode != http.StatusOK || jsonString(m, "error") != "" {
		status := resp.StatusCode
		if status == http.StatusOK {
			status = http.StatusBadRequest // GitHub answers errors with 200
		}
		return nil, &tokenError{Status: status, Code: jsonString(m, "error"), Desc: truncate(jsonString(m, "error_description"), 200)}
	}
	ts := &platformTokens{Extra: m}
	ts.AccessToken = jsonString(m, "access_token")
	ts.RefreshToken = jsonString(m, "refresh_token")
	ts.TokenType = jsonString(m, "token_type")
	ts.Scope = strings.Join(splitGranted(jsonString(m, "scope")), " ")
	if ts.AccessToken == "" {
		return nil, errors.New("connectors: token endpoint answered without an access token")
	}
	if ts.TokenType == "" || strings.EqualFold(ts.TokenType, "bearer") {
		ts.TokenType = "Bearer"
	}
	if secs := parseExpiresIn(json.RawMessage(jsonString(m, "expires_in"))); secs > 0 {
		t := time.Now().Add(time.Duration(secs) * time.Second).UTC()
		ts.ExpiresAt = &t
	}
	// Tokens never travel further than this struct: drop them from Extra.
	delete(m, "access_token")
	delete(m, "refresh_token")
	delete(m, "id_token")
	return ts, nil
}

// decodeJSON reads a small JSON body (1 MiB at most).
func decodeJSON(r io.Reader, dst any) error {
	return json.NewDecoder(io.LimitReader(r, 1<<20)).Decode(dst)
}

// userInfoResult names the account behind a token.
type userInfoResult struct {
	Account, AccountID string
	Header             http.Header
}

// fetchUserInfo calls spec with token. instance is the Salesforce instance;
// extra the token answer (Notion names the workspace there; Salesforce the
// identity URL).
func (s *Service) fetchUserInfo(ctx context.Context, p *provider, spec userInfoSpec, token, base, instance string, extra map[string]any) (*userInfoResult, error) {
	var target string
	switch {
	case spec.FromIDField != "":
		target = jsonString(extra, spec.FromIDField)
		if err := p.checkIdentityURL(target, instance); err != nil {
			return nil, err
		}
	default:
		target = p.expand(spec.URL, base, instance, map[string]string{"token": token})
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", appName)
	for k, v := range p.DefaultHeaders {
		req.Header.Set(k, v)
	}
	if !spec.NoAuth {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.tokenClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, errUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("connectors: %s user info answered %d", p.ID, resp.StatusCode)
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("connectors: %s user info isn't JSON", p.ID)
	}
	out := &userInfoResult{Header: resp.Header}
	for _, k := range spec.AccountKeys {
		if a := jsonPath(v, k); a != "" {
			out.Account = spec.AccountPrefix + a
			break
		}
	}
	var ids []string
	for _, k := range spec.IDKeys {
		if id := jsonPath(v, k); id != "" {
			ids = append(ids, id)
			if spec.FromIDField == "" {
				break // first match names the account; Salesforce joins org and user
			}
		}
	}
	out.AccountID = strings.Join(ids, "/")
	if out.AccountID == "" {
		return nil, fmt.Errorf("connectors: %s user info has no account id", p.ID)
	}
	if out.Account == "" {
		out.Account = out.AccountID
	}
	out.Account, out.AccountID = truncate(out.Account, 200), truncate(out.AccountID, 200)
	return out, nil
}

// salesforceHosts are the domains a Salesforce instance or identity URL
// may live on.
var salesforceHosts = []string{".salesforce.com", ".force.com", ".cloudforce.com", ".salesforce.mil", ".sfcrmproducts.cn"}

// checkInstanceURL accepts a Salesforce instance URL from a token answer:
// https on a Salesforce domain, or (development) the configured login host.
func (p *provider) checkInstanceURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return errors.New("connectors: the instance URL isn't a URL")
	}
	if login, err := url.Parse(p.vars["login"]); err == nil && strings.EqualFold(login.Host, u.Host) && login.Scheme == u.Scheme {
		return nil
	}
	if u.Scheme != "https" {
		return errors.New("connectors: the instance URL isn't https")
	}
	h := strings.ToLower(u.Hostname())
	for _, sfx := range salesforceHosts {
		if strings.HasSuffix(h, sfx) {
			return nil
		}
	}
	return fmt.Errorf("connectors: %s isn't a Salesforce address", h)
}

// checkIdentityURL accepts Salesforce's identity URL: the login host, the
// instance, or another Salesforce domain.
func (p *provider) checkIdentityURL(raw, instance string) error {
	if raw == "" {
		return errors.New("connectors: the token answer has no identity URL")
	}
	if instance != "" {
		if iu, err := url.Parse(instance); err == nil {
			if u, err := url.Parse(raw); err == nil && strings.EqualFold(u.Host, iu.Host) && u.Scheme == iu.Scheme {
				return nil
			}
		}
	}
	return p.checkInstanceURL(raw)
}

// revokeStyled sends one queued revocation.
func (s *Service) revokeStyled(ctx context.Context, style, endpoint string, client oauthClient, token, hint string) error {
	var req *http.Request
	var err error
	okStatus := func(code int) bool { return code >= 200 && code < 300 }
	switch style {
	case "", "rfc7009":
		return s.revokeToken(ctx, endpoint, client, token, hint)
	case "github_grant": // DELETE /applications/{client_id}/grant, Basic client credentials
		b, _ := json.Marshal(map[string]string{"access_token": token})
		req, err = http.NewRequestWithContext(ctx, http.MethodDelete, endpoint, bytes.NewReader(b))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/vnd.github+json")
			req.SetBasicAuth(client.ID, client.Secret)
		}
		okStatus = func(code int) bool {
			return code == http.StatusNoContent || code == http.StatusNotFound || code == http.StatusOK
		}
	case "json_basic": // Notion: POST {token} with Basic client credentials
		b, _ := json.Marshal(map[string]string{"token": token})
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			req.SetBasicAuth(client.ID, client.Secret)
		}
	case "path_delete": // HubSpot: DELETE <endpoint><token>
		req, err = http.NewRequestWithContext(ctx, http.MethodDelete, endpoint+url.PathEscape(token), nil)
		okStatus = func(code int) bool {
			return code == http.StatusNoContent || code == http.StatusNotFound || code == http.StatusOK
		}
	default:
		return fmt.Errorf("connectors: unknown revocation style %q", style)
	}
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", appName)
	resp, err := s.tokenClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if !okStatus(resp.StatusCode) {
		return fmt.Errorf("connectors: revocation (%s) answered %d", style, resp.StatusCode)
	}
	return nil
}
