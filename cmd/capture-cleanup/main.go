// capture-cleanup is a one-time offline maintenance command for redundant post content. Stop Stash and retain a current database backup before use.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/sqlite"
)

func main() {
	path := flag.String("database", "", "existing native Stash database (Stash must be stopped)")
	apply := flag.Bool("apply", false, "share unchanged post content and discard redundant payloads in one transaction")
	postText := flag.Bool("discard-caption-history", false, "replace obsolete caption copies with saved post content and remove unreferenced revisions")
	flag.Parse()
	if !*apply || *path == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*path, *postText); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(path string, postText bool) error {
	if _, err := os.Stat(path); err != nil {
		return err
	}
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	if err := db.Open(path); err != nil {
		var migration *sqlite.MigrationNeededError
		if !errors.As(err, &migration) {
			return err
		}
		if err := db.RunAllMigrations(); err != nil {
			return err
		}
		if err := db.ReInitialise(); err != nil {
			return err
		}
	}
	defer db.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	cleanup := db.CompactCaptureContent
	if postText {
		cleanup = db.CompactPostText
	}
	result, err := cleanup(ctx, func(phase string, n int) { fmt.Fprintf(os.Stderr, "%s: %d\n", phase, n) })
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
