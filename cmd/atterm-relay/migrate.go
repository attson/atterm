package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/attson/atterm/internal/userstore"
)

// migrateCmd implements `atterm-relay migrate --from <dsn> --to <dsn>`:
// a one-shot, offline, faithful copy of all data from one userstore backend
// to another (for SQLite<->Postgres switching). The target must be empty.
func migrateCmd(args []string) int {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	from := fs.String("from", "", "source DSN: sqlite:<path> | postgres://...")
	to := fs.String("to", "", "target DSN (must be empty): sqlite:<path> | postgres://...")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *from == "" || *to == "" {
		log.Printf("migrate: --from and --to are required")
		fs.Usage()
		return 2
	}

	ctx := context.Background()
	src, err := userstore.OpenFromDSN(ctx, *from)
	if err != nil {
		log.Printf("migrate: open --from: %v", err)
		return 1
	}
	defer src.Close()
	dst, err := userstore.OpenFromDSN(ctx, *to)
	if err != nil {
		log.Printf("migrate: open --to: %v", err)
		return 1
	}
	defer dst.Close()

	counts, err := userstore.Copy(ctx, src, dst)
	if err != nil {
		log.Printf("migrate: %v", err)
		return 1
	}

	var total int64
	fmt.Println("migrate: copied rows per table:")
	for _, table := range userstore.CopyTableOrder() {
		fmt.Printf("  %-24s %d\n", table, counts[table])
		total += counts[table]
	}
	fmt.Printf("migrate: done (%d rows across %d tables)\n", total, len(counts))
	return 0
}
