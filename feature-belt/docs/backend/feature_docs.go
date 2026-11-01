package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/docs"
)

// docs: Notion-style pages and databases (feature-belt/docs). Requires the
// access, orgs, notifications and moderation features.
func init() { RegisterModule(func(d *app.Deps) app.Module { return docs.New(d) }) }
