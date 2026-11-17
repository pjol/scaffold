package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/forum"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return forum.New(d) }) }
