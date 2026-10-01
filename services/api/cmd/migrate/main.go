// Command migrate applies or rolls back database migrations.
//
// Usage:
//
//	migrate [up | -up]           apply all pending up-migrations
//	migrate [down | -down [N]]   roll back N migrations (default 1; capped at
//	                             the number applied)
//
// DATABASE_URL is read from the environment; SKOLARA_MIGRATIONS_DIR overrides
// the migrations directory (default "./migrations"). Calling the command with
// no argument is equivalent to "up" (historical default).
//
// Directions may be given positionally ("up", "down") or as flags ("-up",
// "-down N"). The positional forms are kept for compatibility with the CI
// migrations job and existing scripts; "down" without a count steps back
// exactly one migration.
//
// Exit codes:
//
//	0  success — including the documented "nothing to do" no-ops: up on a
//	   fully migrated database, and down when no migrations are applied
//	   (e.g. a second down after reaching a clean database)
//	1  real failure — DATABASE_URL missing, or a migration error
//	2  usage error — unknown direction/argument, combining up and down,
//	   or a -down count that is not an integer >= 1
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

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

	var (
		up        bool
		downSteps = -1 // -1 = down not requested
	)
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "up", "-up":
			up = true
		case "down", "-down":
			downSteps = 1
			// Optional trailing count: "down 3" / "-down 3".
			if i+1 < len(args) {
				if n, err := strconv.Atoi(args[i+1]); err == nil {
					if n < 1 {
						fmt.Fprintf(os.Stderr, "migrate: down count must be >= 1, got %d\n", n)
						os.Exit(2)
					}
					downSteps = n
					i++
				}
			}
		default:
			fmt.Fprintf(os.Stderr, "migrate: unknown argument %q (use up | -up | down [N] | -down [N])\n", args[i])
			os.Exit(2)
		}
	}
	if up && downSteps >= 0 {
		fmt.Fprintln(os.Stderr, "migrate: cannot combine up and down")
		os.Exit(2)
	}
	// No direction at all keeps the historical default: up.
	if !up && downSteps < 0 {
		up = true
	}

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		fmt.Fprintln(os.Stderr, "migrate: DATABASE_URL not set")
		os.Exit(1)
	}

	var err error
	if up {
		err = postgres.MigrateUp(url, dir)
	} else {
		err = postgres.MigrateDown(url, dir, downSteps)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
	fmt.Println("migrate: ok")
}
