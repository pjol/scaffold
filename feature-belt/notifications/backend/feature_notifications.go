package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/notifications"
)

// The notifications feature replaces Deps.Notify before any module is built,
// so every module's d.Notify.Send gains the inbox, push, email and
// preferences without changing.
func init() {
	RegisterDeps(func(d *app.Deps) { d.Notify = notifications.NewSender(d) })
	RegisterModule(func(d *app.Deps) app.Module { return notifications.New(d) })
}
