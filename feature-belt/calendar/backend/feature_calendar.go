package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/calendar"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return calendar.New(d) }) }
