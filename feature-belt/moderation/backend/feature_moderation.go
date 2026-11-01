package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/moderation"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return moderation.New(d) }) }
