package connectors

import (
	"encoding/json"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"scaffoldapp/backend/internal/config"
)

// The platform registry: every outside platform people can connect once and
// use from any feature (app.Connections). Each entry is data — OAuth
// endpoints, how scopes combine, refresh and revocation, the user-info call
// that names the account, API bases by name and the hosts a Client may
// reach — so adding a platform is adding an entry (plus its env vars in
// feature.json). Deployments register their own OAuth app with each
// platform (the consent screen shows the app's name) and set its client id
// and secret; without them the platform says "Not set up" and, where the
// platform has personal tokens, people can still paste one.
//
// URL templates may use {base} (GitLab: the connection's base URL),
// {login} (SALESFORCE_LOGIN_URL), {tenant} (MICROSOFT_TENANT), {instance}
// (the Salesforce instance from the token answer), {client_id} and {token}.

// provider is one platform.
type provider struct {
	ID   string
	Name string

	// OAuth 2.0 authorization code flow.
	AuthorizeURL string
	TokenURL     string
	TokenAuth    string            // client_secret_basic | client_secret_post
	TokenJSON    bool              // the token endpoint takes JSON (Notion)
	PKCE         bool              // S256 code challenge
	ScopeParam   bool              // false: the platform has no scope parameter (Notion: pages are picked on consent)
	BaseScopes   []string          // always requested: identity and refresh
	Incremental  bool              // ask for only the new scopes; the platform keeps earlier grants (Google include_granted_scopes)
	AuthParams   map[string]string // fixed extra authorization parameters
	RefreshScope bool              // send the granted scopes with a refresh (Microsoft v2)
	ScopeImplies map[string][]string
	ScopeFold    func(string) string // normalises a scope before comparing
	ScopeNotes   map[string]string   // what a scope really grants, shown next to capabilities

	// Revocation on disconnect.
	RevokeURL   string
	RevokeStyle string // rfc7009 | github_grant | json_basic | path_delete; "" = the platform can't revoke (remove the app there)
	RevokeWhich string // refresh | access | both (refresh falls back to access when there is none)

	// Naming the account.
	UserInfo  userInfoSpec
	FromToken func(extra map[string]any) (account, accountID string) // account named by the token answer (Notion)

	// Reaching the API.
	Endpoints      map[string]string // name → URL template
	API            []hostRule        // requests here carry the token
	Content        []hostRule        // reachable without the token (pre-signed downloads, raw files)
	DefaultHeaders map[string]string // set when the caller didn't
	Git            *gitSpec          // smart-HTTP clone through Client (repos)

	// Personal / private tokens, where the platform has them.
	Token *tokenSpec

	SelfManaged bool // GitLab: a base URL per connection (personal tokens may name any instance)
	Instance    bool // Salesforce: the token answer names the instance (instance_url)
	Docs        string

	// Env: client id/secret and per-platform settings, read by loadProviders.
	EnvClientID, EnvClientSecret string
	Webhook                      *webhookSpec

	// Filled at boot.
	clientID, clientSecret string
	vars                   map[string]string // {login}, {tenant}, {base}
	override               *url.URL          // CONNECTORS_DEV_PROVIDER_URLS (development only)
}

// hostRule matches a host exactly ("api.github.com") or by suffix
// (".googleapis.com"), under a path prefix.
type hostRule struct {
	Host   string
	Prefix string // "" = any path
	// SegSuffix (development overrides only): the path segment after Prefix
	// names the original host, which must end with this suffix.
	SegSuffix string
}

// userInfoSpec is the call that names the account behind a token.
type userInfoSpec struct {
	URL           string   // template; {token} for HubSpot's token-info call
	NoAuth        bool     // the token is in the URL, not a header
	AccountKeys   []string // dotted JSON paths, first non-empty wins
	IDKeys        []string // joined with "/" when several
	AccountPrefix string   // "Portal " for HubSpot private apps
	FromIDField   string   // Salesforce: the token answer's field holding the identity URL
}

// tokenSpec describes personal or private tokens.
type tokenSpec struct {
	Label       string // "Personal access token"
	HelpURL     string // where people create one
	UserInfo    userInfoSpec
	ScopeHeader string // GitHub classic tokens list scopes in X-OAuth-Scopes
	ScopesURL   string // GitLab: the token's own scopes
	Prefixes    []string
}

// gitSpec allows git smart-HTTP (fetch only) on the platform's web host.
type gitSpec struct {
	User   string   // Basic auth user: x-access-token (GitHub), oauth2 (GitLab)
	Web    string   // endpoint name of the web host ("web")
	Scopes []string // any of these lets a connection fetch over git
}

// webhookSpec says how a platform signs webhooks.
type webhookSpec struct {
	EnvSecret string
	Verify    string // github_hmac | gitlab_token | hubspot_v3 | zoom_hmac | google_channel | microsoft_state | notion_hmac
}

var googleScopeAlias = map[string]string{
	"email":   "https://www.googleapis.com/auth/userinfo.email",
	"profile": "https://www.googleapis.com/auth/userinfo.profile",
}

func registry() []*provider {
	return []*provider{
		{
			ID: "google", Name: "Google",
			AuthorizeURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token",
			TokenAuth: "client_secret_post", PKCE: true, ScopeParam: true, Incremental: true,
			BaseScopes: []string{"openid", "https://www.googleapis.com/auth/userinfo.email", "https://www.googleapis.com/auth/userinfo.profile"},
			AuthParams: map[string]string{"access_type": "offline", "include_granted_scopes": "true"},
			ScopeFold: func(s string) string {
				if a, ok := googleScopeAlias[s]; ok {
					return a
				}
				return s
			},
			ScopeImplies: map[string][]string{
				"https://www.googleapis.com/auth/drive":     {"https://www.googleapis.com/auth/drive.readonly", "https://www.googleapis.com/auth/drive.file", "https://www.googleapis.com/auth/drive.metadata.readonly"},
				"https://www.googleapis.com/auth/calendar":  {"https://www.googleapis.com/auth/calendar.readonly", "https://www.googleapis.com/auth/calendar.events", "https://www.googleapis.com/auth/calendar.events.readonly"},
				"https://www.googleapis.com/auth/documents": {"https://www.googleapis.com/auth/documents.readonly"},
			},
			RevokeURL: "https://oauth2.googleapis.com/revoke", RevokeStyle: "rfc7009", RevokeWhich: "refresh",
			UserInfo: userInfoSpec{URL: "https://openidconnect.googleapis.com/v1/userinfo", AccountKeys: []string{"email"}, IDKeys: []string{"sub"}},
			Endpoints: map[string]string{
				"drive": "https://www.googleapis.com/drive/v3", "drive_upload": "https://www.googleapis.com/upload/drive/v3",
				"docs": "https://docs.googleapis.com/v1", "sheets": "https://sheets.googleapis.com/v4",
				"calendar": "https://www.googleapis.com/calendar/v3", "people": "https://people.googleapis.com/v1",
				"meet": "https://meet.googleapis.com/v2",
			},
			API:         []hostRule{{Host: ".googleapis.com"}},
			Content:     []hostRule{{Host: ".googleusercontent.com"}},
			Docs:        "https://console.cloud.google.com/apis/credentials",
			EnvClientID: "GOOGLE_CLIENT_ID", EnvClientSecret: "GOOGLE_CLIENT_SECRET",
			Webhook: &webhookSpec{EnvSecret: "GOOGLE_WEBHOOK_TOKEN", Verify: "google_channel"},
		},
		{
			ID: "microsoft", Name: "Microsoft",
			AuthorizeURL: "https://login.microsoftonline.com/{tenant}/oauth2/v2.0/authorize",
			TokenURL:     "https://login.microsoftonline.com/{tenant}/oauth2/v2.0/token",
			TokenAuth:    "client_secret_post", PKCE: true, ScopeParam: true, Incremental: true, RefreshScope: true,
			BaseScopes: []string{"openid", "profile", "email", "offline_access", "User.Read"},
			AuthParams: map[string]string{"response_mode": "query"},
			ScopeFold: func(s string) string {
				return strings.ToLower(strings.TrimPrefix(s, "https://graph.microsoft.com/"))
			},
			ScopeImplies: map[string][]string{
				"files.readwrite.all": {"files.read.all", "files.readwrite", "files.read"},
				"files.read.all":      {"files.read"},
				"files.readwrite":     {"files.read"},
				"sites.readwrite.all": {"sites.read.all"},
				"calendars.readwrite": {"calendars.read"},
			},
			// Microsoft has no token revocation endpoint: people remove the app at myapps.microsoft.com.
			UserInfo:    userInfoSpec{URL: "https://graph.microsoft.com/v1.0/me", AccountKeys: []string{"mail", "userPrincipalName"}, IDKeys: []string{"id"}},
			Endpoints:   map[string]string{"graph": "https://graph.microsoft.com/v1.0"},
			API:         []hostRule{{Host: "graph.microsoft.com"}},
			Content:     []hostRule{{Host: ".sharepoint.com"}, {Host: ".1drv.com"}, {Host: ".svc.ms"}, {Host: ".files.1drv.com"}},
			Docs:        "https://entra.microsoft.com/#view/Microsoft_AAD_RegisteredApps/ApplicationsListBlade",
			EnvClientID: "MICROSOFT_CLIENT_ID", EnvClientSecret: "MICROSOFT_CLIENT_SECRET",
			Webhook: &webhookSpec{EnvSecret: "MICROSOFT_WEBHOOK_CLIENT_STATE", Verify: "microsoft_state"},
		},
		{
			ID: "github", Name: "GitHub",
			AuthorizeURL: "https://github.com/login/oauth/authorize", TokenURL: "https://github.com/login/oauth/access_token",
			TokenAuth: "client_secret_post", PKCE: true, ScopeParam: true,
			ScopeImplies: map[string][]string{
				"repo":      {"public_repo", "repo:status", "repo_deployment", "repo:invite", "security_events"},
				"admin:org": {"write:org", "read:org"},
				"write:org": {"read:org"},
				"user":      {"read:user", "user:email", "user:follow"},
				"workflow":  {},
			},
			ScopeNotes: map[string]string{
				"repo":     "full read and write access to every repository you can reach, public and private (GitHub has no read-only scope for private repositories)",
				"workflow": "change GitHub Actions workflow files",
				"read:org": "read your organization and team memberships",
			},
			RevokeURL: "https://api.github.com/applications/{client_id}/grant", RevokeStyle: "github_grant", RevokeWhich: "access",
			UserInfo: userInfoSpec{URL: "https://api.github.com/user", AccountKeys: []string{"login"}, IDKeys: []string{"id"}},
			Endpoints: map[string]string{
				"api": "https://api.github.com", "web": "https://github.com", "graphql": "https://api.github.com/graphql",
				"uploads": "https://uploads.github.com",
			},
			API:            []hostRule{{Host: "api.github.com"}, {Host: "uploads.github.com"}},
			Content:        []hostRule{{Host: "codeload.github.com"}, {Host: "raw.githubusercontent.com"}, {Host: "objects.githubusercontent.com"}, {Host: ".githubusercontent.com"}},
			DefaultHeaders: map[string]string{"Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28"},
			Git:            &gitSpec{User: "x-access-token", Web: "web", Scopes: []string{"repo", "public_repo"}},
			Token: &tokenSpec{
				Label: "Personal access token", HelpURL: "https://github.com/settings/personal-access-tokens",
				UserInfo:    userInfoSpec{URL: "https://api.github.com/user", AccountKeys: []string{"login"}, IDKeys: []string{"id"}},
				ScopeHeader: "X-OAuth-Scopes", Prefixes: []string{"ghp_", "github_pat_", "gho_", "ghu_"},
			},
			Docs:        "https://github.com/settings/developers",
			EnvClientID: "GITHUB_CLIENT_ID", EnvClientSecret: "GITHUB_CLIENT_SECRET",
			Webhook: &webhookSpec{EnvSecret: "GITHUB_WEBHOOK_SECRET", Verify: "github_hmac"},
		},
		{
			ID: "gitlab", Name: "GitLab", SelfManaged: true,
			AuthorizeURL: "{base}/oauth/authorize", TokenURL: "{base}/oauth/token",
			TokenAuth: "client_secret_post", PKCE: true, ScopeParam: true,
			BaseScopes: []string{"read_user"},
			ScopeImplies: map[string][]string{
				"api":              {"read_api", "read_user"},
				"write_repository": {"read_repository"},
			},
			ScopeNotes: map[string]string{
				"api": "full read and write access to the API for every group and project you can reach",
			},
			RevokeURL: "{base}/oauth/revoke", RevokeStyle: "rfc7009", RevokeWhich: "both",
			UserInfo:  userInfoSpec{URL: "{base}/api/v4/user", AccountKeys: []string{"username"}, IDKeys: []string{"id"}},
			Endpoints: map[string]string{"api": "{base}/api/v4", "web": "{base}", "graphql": "{base}/api/graphql"},
			API:       []hostRule{{Host: "{base}", Prefix: "/api/"}},
			Git:       &gitSpec{User: "oauth2", Web: "web", Scopes: []string{"read_repository", "write_repository"}},
			Token: &tokenSpec{
				Label: "Personal access token", HelpURL: "{base}/-/user_settings/personal_access_tokens",
				UserInfo:  userInfoSpec{URL: "{base}/api/v4/user", AccountKeys: []string{"username"}, IDKeys: []string{"id"}},
				ScopesURL: "{base}/api/v4/personal_access_tokens/self", Prefixes: []string{"glpat-"},
			},
			Docs:        "{base}/-/user_settings/applications",
			EnvClientID: "GITLAB_CLIENT_ID", EnvClientSecret: "GITLAB_CLIENT_SECRET",
			Webhook: &webhookSpec{EnvSecret: "GITLAB_WEBHOOK_SECRET", Verify: "gitlab_token"},
		},
		{
			ID: "notion", Name: "Notion",
			AuthorizeURL: "https://api.notion.com/v1/oauth/authorize", TokenURL: "https://api.notion.com/v1/oauth/token",
			TokenAuth: "client_secret_basic", TokenJSON: true,
			AuthParams: map[string]string{"owner": "user"},
			RevokeURL:  "https://api.notion.com/v1/oauth/revoke", RevokeStyle: "json_basic", RevokeWhich: "access",
			FromToken: func(extra map[string]any) (string, string) {
				return jsonString(extra, "workspace_name"), jsonString(extra, "workspace_id")
			},
			Endpoints:      map[string]string{"api": "https://api.notion.com/v1"},
			API:            []hostRule{{Host: "api.notion.com", Prefix: "/v1/"}},
			Content:        []hostRule{{Host: "prod-files-secure.s3.us-west-2.amazonaws.com"}, {Host: "file.notion.so"}},
			DefaultHeaders: map[string]string{"Notion-Version": "2022-06-28"},
			Token: &tokenSpec{
				Label: "Internal integration token", HelpURL: "https://www.notion.so/profile/integrations",
				UserInfo: userInfoSpec{URL: "https://api.notion.com/v1/users/me", AccountKeys: []string{"bot.workspace_name", "name"}, IDKeys: []string{"bot.workspace_id", "id"}},
				Prefixes: []string{"ntn_", "secret_"},
			},
			Docs:        "https://www.notion.so/profile/integrations",
			EnvClientID: "NOTION_CLIENT_ID", EnvClientSecret: "NOTION_CLIENT_SECRET",
			Webhook: &webhookSpec{EnvSecret: "NOTION_WEBHOOK_SECRET", Verify: "notion_hmac"},
		},
		{
			ID: "hubspot", Name: "HubSpot",
			AuthorizeURL: "https://app.hubspot.com/oauth/authorize", TokenURL: "https://api.hubapi.com/oauth/v1/token",
			TokenAuth: "client_secret_post", ScopeParam: true,
			BaseScopes: []string{"oauth"},
			RevokeURL:  "https://api.hubapi.com/oauth/v1/refresh-tokens/{token}", RevokeStyle: "path_delete", RevokeWhich: "refresh",
			UserInfo:  userInfoSpec{URL: "https://api.hubapi.com/oauth/v1/access-tokens/{token}", NoAuth: true, AccountKeys: []string{"hub_domain", "user"}, IDKeys: []string{"hub_id"}},
			Endpoints: map[string]string{"api": "https://api.hubapi.com"},
			API:       []hostRule{{Host: "api.hubapi.com"}, {Host: ".hubapi.com"}},
			Token: &tokenSpec{
				Label: "Private app access token", HelpURL: "https://developers.hubspot.com/docs/api/private-apps",
				UserInfo: userInfoSpec{URL: "https://api.hubapi.com/account-info/v3/details", AccountKeys: []string{"portalId"}, IDKeys: []string{"portalId"}, AccountPrefix: "Portal "},
				Prefixes: []string{"pat-"},
			},
			Docs:        "https://developers.hubspot.com/docs/api/creating-an-app",
			EnvClientID: "HUBSPOT_CLIENT_ID", EnvClientSecret: "HUBSPOT_CLIENT_SECRET",
			Webhook: &webhookSpec{EnvSecret: "HUBSPOT_CLIENT_SECRET", Verify: "hubspot_v3"},
		},
		{
			ID: "salesforce", Name: "Salesforce", Instance: true,
			AuthorizeURL: "{login}/services/oauth2/authorize", TokenURL: "{login}/services/oauth2/token",
			TokenAuth: "client_secret_post", PKCE: true, ScopeParam: true,
			BaseScopes:   []string{"refresh_token", "id"},
			ScopeImplies: map[string][]string{"full": {"api", "id", "refresh_token"}},
			RevokeURL:    "{login}/services/oauth2/revoke", RevokeStyle: "rfc7009", RevokeWhich: "refresh",
			UserInfo:    userInfoSpec{FromIDField: "id", AccountKeys: []string{"username"}, IDKeys: []string{"organization_id", "user_id"}},
			Endpoints:   map[string]string{"instance": "{instance}"},
			API:         []hostRule{{Host: "{instance}", Prefix: "/services/"}},
			Docs:        "https://help.salesforce.com/s/articleView?id=sf.connected_app_create.htm",
			EnvClientID: "SALESFORCE_CLIENT_ID", EnvClientSecret: "SALESFORCE_CLIENT_SECRET",
		},
		{
			ID: "zoom", Name: "Zoom",
			AuthorizeURL: "https://zoom.us/oauth/authorize", TokenURL: "https://zoom.us/oauth/token",
			TokenAuth: "client_secret_basic", PKCE: true, ScopeParam: true,
			BaseScopes: []string{"user:read:user"},
			RevokeURL:  "https://zoom.us/oauth/revoke", RevokeStyle: "rfc7009", RevokeWhich: "access",
			UserInfo:    userInfoSpec{URL: "https://api.zoom.us/v2/users/me", AccountKeys: []string{"email"}, IDKeys: []string{"id"}},
			Endpoints:   map[string]string{"api": "https://api.zoom.us/v2"},
			API:         []hostRule{{Host: "api.zoom.us"}},
			Docs:        "https://marketplace.zoom.us/develop/create",
			EnvClientID: "ZOOM_CLIENT_ID", EnvClientSecret: "ZOOM_CLIENT_SECRET",
			Webhook: &webhookSpec{EnvSecret: "ZOOM_WEBHOOK_SECRET", Verify: "zoom_hmac"},
		},
	}
}

// providerEnv reads each platform's settings. Names are literal so the
// scaffold doctor sees every variable.
func providerEnv() map[string]map[string]string {
	return map[string]map[string]string{
		"google":    {"id": config.String("GOOGLE_CLIENT_ID", ""), "secret": config.String("GOOGLE_CLIENT_SECRET", ""), "webhook": config.String("GOOGLE_WEBHOOK_TOKEN", "")},
		"microsoft": {"id": config.String("MICROSOFT_CLIENT_ID", ""), "secret": config.String("MICROSOFT_CLIENT_SECRET", ""), "tenant": config.String("MICROSOFT_TENANT", "common"), "webhook": config.String("MICROSOFT_WEBHOOK_CLIENT_STATE", "")},
		"github":    {"id": config.String("GITHUB_CLIENT_ID", ""), "secret": config.String("GITHUB_CLIENT_SECRET", ""), "webhook": config.String("GITHUB_WEBHOOK_SECRET", "")},
		"gitlab":    {"id": config.String("GITLAB_CLIENT_ID", ""), "secret": config.String("GITLAB_CLIENT_SECRET", ""), "base": config.String("GITLAB_BASE_URL", "https://gitlab.com"), "webhook": config.String("GITLAB_WEBHOOK_SECRET", "")},
		"notion":    {"id": config.String("NOTION_CLIENT_ID", ""), "secret": config.String("NOTION_CLIENT_SECRET", ""), "webhook": config.String("NOTION_WEBHOOK_SECRET", "")},
		"hubspot":   {"id": config.String("HUBSPOT_CLIENT_ID", ""), "secret": config.String("HUBSPOT_CLIENT_SECRET", "")},
		"salesforce": {"id": config.String("SALESFORCE_CLIENT_ID", ""), "secret": config.String("SALESFORCE_CLIENT_SECRET", ""),
			"login": config.String("SALESFORCE_LOGIN_URL", "https://login.salesforce.com")},
		"zoom": {"id": config.String("ZOOM_CLIENT_ID", ""), "secret": config.String("ZOOM_CLIENT_SECRET", ""), "webhook": config.String("ZOOM_WEBHOOK_SECRET", "")},
	}
}

// loadProviders builds the registry with this deployment's settings.
func loadProviders(dev bool) (map[string]*provider, []string) {
	env := providerEnv()
	overrides := map[string]*url.URL{}
	for _, item := range config.List("CONNECTORS_DEV_PROVIDER_URLS", "") {
		id, raw, ok := strings.Cut(item, "=")
		u, err := url.Parse(strings.TrimSpace(raw))
		if !ok || err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			config.Problem("CONNECTORS_DEV_PROVIDER_URLS: %q is not provider=http(s)://host", item)
			continue
		}
		if !dev {
			config.Problem("CONNECTORS_DEV_PROVIDER_URLS is only allowed when APP_ENV=development")
			break
		}
		overrides[strings.TrimSpace(id)] = u
	}
	out := map[string]*provider{}
	var order []string
	for _, p := range registry() {
		e := env[p.ID]
		p.clientID, p.clientSecret = e["id"], e["secret"]
		p.vars = map[string]string{"tenant": e["tenant"], "login": strings.TrimSuffix(e["login"], "/"), "base": strings.TrimSuffix(e["base"], "/")}
		if (p.clientID == "") != (p.clientSecret == "") {
			config.Problem("%s and %s are set together (or neither)", p.EnvClientID, p.EnvClientSecret)
		}
		for _, k := range []string{"login", "base"} {
			if v := p.vars[k]; v != "" {
				u, err := url.Parse(v)
				if err != nil || u.Host == "" || (u.Scheme != "https" && !(dev && u.Scheme == "http")) {
					config.Problem("%s: %q must be an https URL", map[string]string{"login": "SALESFORCE_LOGIN_URL", "base": "GITLAB_BASE_URL"}[k], v)
				}
			}
		}
		if o, ok := overrides[p.ID]; ok {
			p.override = o
		}
		out[p.ID] = p
		order = append(order, p.ID)
	}
	for id := range overrides {
		if out[id] == nil {
			config.Problem("CONNECTORS_DEV_PROVIDER_URLS names %q, which isn't a platform here", id)
		}
	}
	return out, order
}

// oauthReady: this deployment registered an OAuth app with the platform.
func (p *provider) oauthReady() bool { return p.clientID != "" && p.clientSecret != "" }

// available: people can connect at all (OAuth, or a personal token).
func (p *provider) available() bool { return p.oauthReady() || p.Token != nil }

// expand fills a URL template for one connection. base is the connection's
// base URL (GitLab; "" = the configured one), instance its Salesforce
// instance. The development override rewrites the result to
// <override>/<original host><path>.
func (p *provider) expand(tmpl, base, instance string, extra map[string]string) string {
	if tmpl == "" {
		return ""
	}
	if base == "" {
		base = p.vars["base"]
	}
	r := strings.NewReplacer("{base}", base, "{login}", p.vars["login"], "{tenant}", url.PathEscape(p.vars["tenant"]),
		"{instance}", strings.TrimSuffix(instance, "/"), "{client_id}", url.PathEscape(p.clientID))
	out := r.Replace(tmpl)
	for k, v := range extra {
		out = strings.ReplaceAll(out, "{"+k+"}", url.PathEscape(v))
	}
	return p.rewrite(out)
}

// page fills a template for a page people open (docs, token settings):
// never rewritten to a development mock.
func (p *provider) page(tmpl string) string {
	if tmpl == "" {
		return ""
	}
	return strings.ReplaceAll(tmpl, "{base}", p.vars["base"])
}

// rewrite applies the development override to an absolute URL. Templates
// built on {base}/{login}/{instance} already point wherever those settings
// say, so only fixed platform hosts move.
func (p *provider) rewrite(raw string) string {
	if p.override == nil {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || p.isConfiguredHost(u.Host) {
		return raw
	}
	o := *p.override
	o.Path = strings.TrimSuffix(o.Path, "/") + "/" + u.Host + u.Path
	o.RawQuery = u.RawQuery
	return o.String()
}

// isConfiguredHost: the host comes from a setting (GitLab base, Salesforce
// login), not from the built-in registry.
func (p *provider) isConfiguredHost(host string) bool {
	for _, k := range []string{"base", "login"} {
		if v := p.vars[k]; v != "" {
			if u, err := url.Parse(v); err == nil && strings.EqualFold(u.Host, host) {
				return true
			}
		}
	}
	return false
}

// endpoints returns the API bases for one connection.
func (p *provider) endpoints(base, instance string) map[string]string {
	out := make(map[string]string, len(p.Endpoints))
	for k, t := range p.Endpoints {
		if strings.Contains(t, "{instance}") && instance == "" {
			continue
		}
		out[k] = p.expand(t, base, instance, nil)
	}
	return out
}

// rules resolves host rules for one connection.
func (p *provider) rules(list []hostRule, base, instance string) []hostRule {
	out := make([]hostRule, 0, len(list))
	for _, r := range list {
		host, prefix := r.Host, r.Prefix
		if strings.HasPrefix(host, "{") {
			u, err := url.Parse(p.expand(host, base, instance, nil))
			if err != nil || u.Host == "" {
				continue
			}
			host, prefix = u.Host, strings.TrimSuffix(u.Path, "/")+prefix
		} else if p.override != nil {
			base := strings.TrimSuffix(p.override.Path, "/") + "/"
			if strings.HasPrefix(host, ".") {
				out = append(out, hostRule{Host: strings.ToLower(p.override.Host), Prefix: base, SegSuffix: host})
				continue
			}
			host, prefix = p.override.Host, base+host+prefix
		}
		out = append(out, hostRule{Host: strings.ToLower(host), Prefix: prefix})
	}
	return out
}

func (r hostRule) match(host, path string) bool {
	host = strings.ToLower(host)
	ok := host == r.Host || (strings.HasPrefix(r.Host, ".") && strings.HasSuffix(host, r.Host))
	if !ok || (r.Prefix != "" && !strings.HasPrefix(path, r.Prefix) && path+"/" != r.Prefix) {
		return false
	}
	if r.SegSuffix != "" {
		seg, _, _ := strings.Cut(strings.TrimPrefix(path, r.Prefix), "/")
		return strings.HasSuffix(strings.ToLower(seg), r.SegSuffix) && !strings.ContainsAny(seg, "@:")
	}
	return true
}

// ---- scopes ------------------------------------------------------------------

// fold normalises one scope for comparison.
func (p *provider) fold(s string) string {
	if p.ScopeFold != nil {
		return p.ScopeFold(s)
	}
	return s
}

// grantedSet expands granted scopes with what they imply.
func (p *provider) grantedSet(granted []string) map[string]bool {
	set := map[string]bool{}
	var add func(string)
	add = func(s string) {
		s = p.fold(s)
		if set[s] {
			return
		}
		set[s] = true
		for _, imp := range p.ScopeImplies[s] {
			add(imp)
		}
	}
	for _, s := range granted {
		add(s)
	}
	return set
}

// missingScopes are the scopes of need not covered by granted.
func (p *provider) missingScopes(granted, need []string) []string {
	if !p.ScopeParam {
		return nil
	}
	set := p.grantedSet(granted)
	var out []string
	for _, s := range need {
		if !set[p.fold(s)] {
			out = append(out, s)
		}
	}
	return out
}

// requestScopes is what an authorization asks for: base scopes plus the
// capability's, and (platforms without incremental consent) everything
// granted before, so the new grant doesn't lose it.
func (p *provider) requestScopes(granted, need []string) []string {
	if !p.ScopeParam {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(list []string) {
		for _, s := range list {
			if s == "" || seen[p.fold(s)] {
				continue
			}
			seen[p.fold(s)] = true
			out = append(out, s)
		}
	}
	add(p.BaseScopes)
	if !p.Incremental {
		add(granted)
	}
	add(need)
	return out
}

// splitGranted parses a token answer's scope (space or comma separated).
func splitGranted(s string) []string {
	f := strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == ',' })
	if f == nil {
		return []string{}
	}
	return f
}

// unionScopes merges scope lists, keeping order.
func unionScopes(lists ...[]string) []string {
	var out []string
	for _, l := range lists {
		for _, s := range l {
			if s != "" && !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
	}
	return out
}

// scopeNote explains what a scope grants, when the registry says.
func (p *provider) scopeNote(s string) string { return p.ScopeNotes[s] }

// ---- small JSON helpers --------------------------------------------------------

// jsonPath reads a dotted path from decoded JSON as a string (numbers too).
func jsonPath(v any, path string) string {
	cur := v
	for part := range strings.SplitSeq(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = m[part]
	}
	switch x := cur.(type) {
	case string:
		return strings.TrimSpace(x)
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

func jsonString(m map[string]any, key string) string { return jsonPath(m, key) }
