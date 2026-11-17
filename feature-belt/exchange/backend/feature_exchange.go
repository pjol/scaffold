package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/exchange"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return exchange.New(d) }) }
