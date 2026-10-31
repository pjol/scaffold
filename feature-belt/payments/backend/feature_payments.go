package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/payments"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return payments.New(d) }) }
