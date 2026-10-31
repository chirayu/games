// Package migrations contains the ordered SQLite schema migrations.
package migrations

import "embed"

// Files is the migration filesystem consumed by Goose.
//
//go:embed *.sql
var Files embed.FS
