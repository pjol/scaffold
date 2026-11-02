package agents

import (
	"regexp"
	"strings"
	"sync"

	"scaffoldapp/backend/internal/app"
)

// Level is an agent scope level on the wire and in the database: off, read,
// suggest, edit (app.AgentLevel in Go code that talks to other features).
type Level string

const (
	LevelOff     Level = "off"
	LevelRead    Level = "read"
	LevelSuggest Level = "suggest"
	LevelEdit    Level = "edit"
)

var levelRank = map[Level]app.AgentLevel{LevelOff: app.AgentOff, LevelRead: app.AgentRead, LevelSuggest: app.AgentSuggest, LevelEdit: app.AgentEdit}

// Valid reports whether l is one of the four levels.
func (l Level) Valid() bool { _, ok := levelRank[l]; return ok }

// Agent converts to app.AgentLevel (unknown values are off: fail closed).
func (l Level) Agent() app.AgentLevel { return levelRank[l] }

// LevelOf converts an app.AgentLevel (or an SQL rank 0–3).
func LevelOf(a app.AgentLevel) Level {
	switch a {
	case app.AgentRead:
		return LevelRead
	case app.AgentSuggest:
		return LevelSuggest
	case app.AgentEdit:
		return LevelEdit
	}
	return LevelOff
}

var levelLabels = map[Level]string{LevelOff: "Off", LevelRead: "Read", LevelSuggest: "Suggest", LevelEdit: "Edit"}

func (l Level) label() string { return levelLabels[l] }

// Scope keys: a module name ("docs"), or one external service
// ("connectors:<connector id>").
var (
	featureRE = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)
	serviceRE = regexp.MustCompile(`^connectors:[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	kindRE    = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)
	idRE      = regexp.MustCompile(`^[A-Za-z0-9_.:/-]{1,200}$`)
)

func validFeature(f string) bool { return featureRE.MatchString(f) || serviceRE.MatchString(f) }

func isService(f string) bool { return strings.HasPrefix(f, "connectors:") }

// kindFeature is the feature a resource kind belongs to: "docs.page" → "docs".
func kindFeature(kind string) string {
	f, _, _ := strings.Cut(kind, ".")
	return f
}

// Feature labels for settings and copy. Modules don't carry a display name,
// so belt features get a readable one here; others are capitalised, and an
// app can name its own modules with SetFeatureLabel.
var (
	labelsMu      sync.RWMutex
	featureLabels = map[string]string{
		"access": "Teams and sharing", "assistant": "Assistant", "calendar": "Calendar", "calls": "Calls",
		"channels": "Channels", "chat": "Chat", "connectors": "Connections", "events": "Events", "feed": "Feed",
		"identity": "Identity", "mcp": "API access", "moderation": "Blocks and reports", "notifications": "Notifications",
		"orgs": "Organizations", "payments": "Payments", "places": "Places", "presence": "Presence", "search": "Search",
		"support": "Support", "wallets": "Wallets",
	}
)

// SetFeatureLabel names a module in agent settings ("crm" → "CRM"). Call it
// from the app's stitching code (a feature_<name>.go init or the module's
// New) before requests are served.
func SetFeatureLabel(feature, label string) {
	if feature != "" && label != "" {
		labelsMu.Lock()
		featureLabels[feature] = label
		labelsMu.Unlock()
	}
}

// Infrastructure modules serve every feature (the API connection itself,
// blocks and reports, the inbox, search), so agent settings don't list them
// next to content features. Hidden is not unrestricted: they follow the
// default level like any feature without its own setting, and a person's own
// setting on one lists it again so it can be changed or cleared. An app
// adjusts the list with HideFeature and ShowFeature.
var hiddenFeatures = map[string]bool{"mcp": true, "moderation": true, "notifications": true, "search": true}

// HideFeature leaves a module out of the agent settings list (it keeps the
// default level). Call it from stitching code before requests are served.
func HideFeature(feature string) {
	labelsMu.Lock()
	hiddenFeatures[feature] = true
	labelsMu.Unlock()
}

// ShowFeature lists a module hidden by default (HideFeature's inverse).
func ShowFeature(feature string) {
	labelsMu.Lock()
	delete(hiddenFeatures, feature)
	labelsMu.Unlock()
}

func featureHidden(feature string) bool {
	labelsMu.RLock()
	defer labelsMu.RUnlock()
	return hiddenFeatures[feature]
}

func featureLabel(feature string) string {
	labelsMu.RLock()
	l, ok := featureLabels[feature]
	labelsMu.RUnlock()
	if ok {
		return l
	}
	if isService(feature) {
		return "Connected service"
	}
	if len(feature) <= 3 {
		return strings.ToUpper(feature)
	}
	return strings.ToUpper(feature[:1]) + strings.ReplaceAll(feature[1:], "_", " ")
}
