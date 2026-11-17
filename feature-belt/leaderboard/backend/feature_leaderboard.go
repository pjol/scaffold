package api

import (
	"scaffoldapp/backend/internal/app"
	"scaffoldapp/backend/internal/leaderboard"
)

func init() { RegisterModule(func(d *app.Deps) app.Module { return leaderboard.New(d) }) }
