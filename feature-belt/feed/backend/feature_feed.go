package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/feed"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return feed.New(d) }) }
