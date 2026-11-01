package api

import (
	"scaffoldapp/backend/internal/access"
	"scaffoldapp/backend/internal/app"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return access.New(d) }) }
