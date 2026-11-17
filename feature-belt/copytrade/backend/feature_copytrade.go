package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/copytrade"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return copytrade.New(d) }) }
