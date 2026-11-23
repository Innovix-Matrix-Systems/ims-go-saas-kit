// Package migrations embeds the versioned goose SQL migrations. The same files
// are the schema source for sqlc, so there is a single schema definition.
package migrations

import "embed"

// Files holds the embedded goose migrations.
//
//go:embed *.sql
var Files embed.FS
