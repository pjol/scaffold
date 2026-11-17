package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/reputation"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return reputation.New(d) }) }
