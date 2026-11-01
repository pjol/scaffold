package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/crm"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return crm.New(d) }) }
