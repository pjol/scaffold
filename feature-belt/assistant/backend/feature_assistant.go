package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/assistant"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return assistant.New(d) }) }
