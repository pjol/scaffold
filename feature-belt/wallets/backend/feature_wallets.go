package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/wallets"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return wallets.New(d) }) }
