package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/places"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return places.New(d) }) }
