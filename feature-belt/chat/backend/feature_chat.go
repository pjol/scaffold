package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/chat"
)

// chat: direct and group messaging (feature-belt/chat). Requires the
// notifications and moderation features.
func init() { RegisterModule(func(d *app.Deps) app.Module { return chat.New(d) }) }
