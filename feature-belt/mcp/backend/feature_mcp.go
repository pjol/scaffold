package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/mcp"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return mcp.New(d) }) }
