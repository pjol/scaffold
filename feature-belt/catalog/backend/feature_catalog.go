package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/catalog"
)

func init() {
	var svc *catalog.Service
	RegisterModule(func(d *app.Deps) app.Module { svc = catalog.New(d); return svc })
	// Global search is a second module: Service.Search is the catalog's own Go API.
	RegisterModule(func(*app.Deps) app.Module { return catalog.NewGlobalSearch(svc) })
}
