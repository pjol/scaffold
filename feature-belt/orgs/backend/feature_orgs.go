package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/orgs"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return orgs.New(d) }) }
