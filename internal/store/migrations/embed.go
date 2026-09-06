// Package migrations embeds goose SQL migrations.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
