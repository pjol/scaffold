package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/events"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return events.New(d) }) }
