// Command migrate runs GPSR schema migrations against MySQL on port 3308.
//
// Usage:
//
//	go run ./cmd/migrate up      # apply all pending migrations (idempotent)
//	go run ./cmd/migrate down    # roll back the most recent migration
//	go run ./cmd/migrate status  # print applied vs pending versions
//
// Connection settings come from the environment (see .env.example); defaults
// match the project standard (127.0.0.1:3308, db gpsr). The migrations
// directory defaults to ./migrations and can be overridden with -dir.
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"

	_ "github.com/go-sql-driver/mysql"

	"github.com/gpsr/backend/internal/config"
	"github.com/gpsr/backend/internal/migrate"
)

func main() {
	dir := flag.String("dir", "migrations", "path to the migrations directory")
	flag.Parse()

	cmd := flag.Arg(0)
	if cmd == "" {
		usageAndExit()
	}

	cfg := config.Load()
	db, err := sql.Open("mysql", cfg.MySQLDSN())
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		log.Fatalf("ping db (is MySQL up on %s:%s?): %v", cfg.DBHost, cfg.DBPort, err)
	}

	switch cmd {
	case "up":
		applied, err := migrate.Up(db, *dir)
		if err != nil {
			log.Fatalf("migrate up: %v", err)
		}
		if len(applied) == 0 {
			fmt.Println("migrate up: nothing to apply (schema already current)")
		} else {
			fmt.Printf("migrate up: applied versions %v\n", applied)
		}
	case "down":
		v, err := migrate.Down(db, *dir)
		if err != nil {
			log.Fatalf("migrate down: %v", err)
		}
		if v == 0 {
			fmt.Println("migrate down: nothing to roll back")
		} else {
			fmt.Printf("migrate down: rolled back version %d\n", v)
		}
	default:
		usageAndExit()
	}
}

func usageAndExit() {
	fmt.Fprintln(os.Stderr, "usage: migrate [-dir migrations] <up|down>")
	os.Exit(2)
}
