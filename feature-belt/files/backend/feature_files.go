package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/files"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return files.New(d) }) }
