package api

import (
	"scaffoldapp/backend/internal/agents"
	"scaffoldapp/backend/internal/app"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return agents.New(d) }) }
