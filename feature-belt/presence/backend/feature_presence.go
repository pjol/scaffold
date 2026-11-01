package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/presence"
)

// presence: online/away/offline from heartbeats, custom status, Do Not
// Disturb (feature-belt/presence).
func init() { RegisterModule(func(d *app.Deps) app.Module { return presence.New(d) }) }
