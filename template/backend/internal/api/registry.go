package api

import "scaffoldapp/backend/internal/app"

// Feature modules register themselves from one file each,
// internal/api/feature_<name>.go, which `scaffold add` drops in:
//
//	package api
//
//	import (
//		"scaffoldapp/backend/internal/app"
//		"scaffoldapp/backend/internal/chat"
//	)
//
//	func init() { RegisterModule(func(d *app.Deps) app.Module { return chat.New(d) }) }
//
// Removing the file unplugs the module (its tables stay). Files run in name
// order, so registration order is stable.

var (
	depProviders []func(*app.Deps)
	moduleCtors  []func(*app.Deps) app.Module
)

// RegisterDeps adds a provider that may replace a shared service on Deps
// (e.g. notifications sets d.Notify) before any module is constructed.
func RegisterDeps(fn func(*app.Deps)) { depProviders = append(depProviders, fn) }

// RegisterModule adds a feature module constructor.
func RegisterModule(fn func(*app.Deps) app.Module) { moduleCtors = append(moduleCtors, fn) }
