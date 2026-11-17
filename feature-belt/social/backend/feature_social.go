package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/social"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return social.New(d) }) }
