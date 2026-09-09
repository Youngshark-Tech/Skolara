// Command migrate applies or rolls back database migrations.
// Usage: migrate [up|down] — DATABASE_URL from environment.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Roy-Wanyoike/Skolara/services/api/internal/platform/postgres"
)

func main() {
	dir := os.Getenv("SKOLARA_MIGRATIONS_DIR")
	if dir == "" {
		dir = "migrations"
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	cmd := "up"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "migrate: DATABASE_URL not set")
		os.Exit(1)
	}
	var err error
	switch cmd {
	case "up":
		err = postgres.MigrateUp(url, dir)
	case "down":
		err = postgres.MigrateDown(url, dir, 1)
	default:
		fmt.Fprintf(os.Stderr, "migrate: unknown command %q (use up|down)\n", cmd)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
	fmt.Println("migrate: ok")
}
