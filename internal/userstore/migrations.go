package userstore

import "embed"

// migrationsFS holds the embedded SQL migration files. Splitting this from
// store.go keeps the embed directive co-located with what it represents
// and keeps store.go focused on DB lifecycle and the migration runner.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS
