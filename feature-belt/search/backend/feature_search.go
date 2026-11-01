package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/search"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return search.New(d) }) }
