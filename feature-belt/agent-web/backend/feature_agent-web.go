package api

import (
	"scaffoldapp/backend/internal/agentweb"
	"scaffoldapp/backend/internal/app"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return agentweb.New(d) }) }
