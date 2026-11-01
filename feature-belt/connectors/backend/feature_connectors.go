package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/connectors"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return connectors.New(d) }) }
