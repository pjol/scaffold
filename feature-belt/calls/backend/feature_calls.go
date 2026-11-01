package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/calls"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return calls.New(d) }) }
