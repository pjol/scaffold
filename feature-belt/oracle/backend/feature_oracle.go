package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/oracle"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return oracle.New(d) }) }
