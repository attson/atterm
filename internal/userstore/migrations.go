package userstore

import "embed"

// migrationsFS holds the embedded SQL migration files, split by dialect
// subdirectory: migrations/sqlite/*.sql and migrations/postgres/*.sql.
// The migration runner reads the subdirectory named by the active
// dialect (see DBStore.migrate).
//
//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
var migrationsFS embed.FS
