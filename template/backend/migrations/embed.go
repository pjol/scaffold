// Package migrations embeds the SQL migrations applied on boot.
//
// Names are NNNN_<track>_<what>.sql and the number decides order. Tracks own
// fixed ranges (concrete/database.md): core 0001–0099, each feature-belt
// module its block of 100 (see its feature.json), app 5000–9999. Never edit
// an applied file; add the next number in the track's range.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
