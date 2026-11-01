package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/support"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return support.New(d) }) }
