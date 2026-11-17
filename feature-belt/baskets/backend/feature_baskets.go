package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/baskets"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return baskets.New(d) }) }
