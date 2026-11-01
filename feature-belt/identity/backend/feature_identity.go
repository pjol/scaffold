package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/identity"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return identity.New(d) }) }
