package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/repos"
)

func init() {
	RegisterModule(func(d *app.Deps) app.Module { return repos.New(d) })
}
