// LEGACY-UPGRADE-ONLY: recover-twitter-albums is offline upgrade tooling for historical
// gallery-dl files. Stop Stash and retain a current backup before use. Gallery
// publication follows through the normal album-backfill API after restart.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/sqlite"
)

func main() {
	database := flag.String("database", "", "existing native database; Stash must be stopped")
	input := flag.String("input", "", "reviewed JSON array of tweet IDs and existing file evidence")
	digest := flag.String("expected-sha256", "", "SHA-256 of the reviewed input")
	mapping := flag.String("path-map", "", "library-prefix=host-prefix for checking existing media")
	apply := flag.Bool("apply", false, "recover missing identities, selected slots and media links")
	flag.Parse()
	if *database == "" || *input == "" || *mapping == "" || len(*digest) != 64 {
		flag.Usage()
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, *database, *input, *digest, *mapping, *apply)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func checkFiles(preview *sqlite.LegacyTwitterRecoveryPreview, from, to string) error {
	for _, slot := range preview.Slots {
		relative, err := filepath.Rel(from, slot.LibraryPath)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			return errors.New("media is outside the reviewed library prefix")
		}
		info, err := os.Stat(filepath.Join(to, relative))
		if err != nil {
			return fmt.Errorf("media file unavailable: %w", err)
		}
		if !info.Mode().IsRegular() || info.Size() != slot.Size {
			return errors.New("media file no longer matches the library record")
		}
	}
	return nil
}

func run(ctx context.Context, database, input, digest, mapping string, apply bool) error {
	info, err := os.Stat(input)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > 64<<20 {
		return errors.New("repair input must be a regular JSON file below 64 MiB")
	}
	body, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	if digest != hex.EncodeToString(sum[:]) {
		return errors.New("repair input differs from the reviewed digest")
	}
	var inputs []sqlite.LegacyTwitterRecovery
	if err := json.Unmarshal(body, &inputs); err != nil {
		return err
	}
	if len(inputs) == 0 || len(inputs) > 10000 {
		return errors.New("repair requires between 1 and 10000 explicit tweet groups")
	}
	from, to, ok := strings.Cut(mapping, "=")
	if !ok || !filepath.IsAbs(from) || !filepath.IsAbs(to) {
		return errors.New("path map requires absolute library and host prefixes")
	}
	if _, err := os.Stat(database); err != nil {
		return err
	}
	config.InitializeEmpty()
	db := sqlite.NewDatabase()
	if err := db.Open(database); err != nil {
		return err // Deliberately never run unrelated schema migrations.
	}
	defer db.Close()
	output := json.NewEncoder(os.Stdout)
	for _, input := range inputs {
		if err := ctx.Err(); err != nil {
			return err
		}
		result := map[string]any{"tweet_id": input.TweetID}
		preview, err := db.PreviewLegacyTwitterRecovery(ctx, input)
		if err == nil && preview.State == "ready" {
			err = checkFiles(preview, from, to)
		}
		if err == nil && apply {
			var post string
			post, err = db.ApplyLegacyTwitterRecovery(ctx, input, preview.Signature)
			result["post_uuid"], result["state"] = post, "recovered"
			if preview.State == "existing" {
				result["state"] = "existing"
			}
		} else if err == nil {
			result["preview"], result["state"] = preview, preview.State
		}
		if err != nil {
			result["state"], result["error"] = "review", err.Error()
		}
		if err := output.Encode(result); err != nil {
			return err
		}
	}
	return nil
}
