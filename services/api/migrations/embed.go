// Package migrations embeds the SQL migration files so binaries ship the
// schema and can self-migrate at startup (no filesystem dependence).
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
