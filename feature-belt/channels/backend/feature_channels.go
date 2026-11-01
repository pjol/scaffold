package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/channels"
)

// channels: org workspaces with channels, DMs, threads and mentions
// (feature-belt/channels). Requires orgs, presence, notifications and moderation.
func init() { RegisterModule(func(d *app.Deps) app.Module { return channels.New(d) }) }
