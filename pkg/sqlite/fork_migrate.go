package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"sort"

	"github.com/stashapp/stash/pkg/logger"
)

const (
	forkSchemaMigrationsTable = "fork_schema_migrations"

	// These versions existed before fork migrations were split out from
	// upstream's schema_migrations table. A local DB at 998 has fork migration 1
	// applied; a local DB at 999 has migrations 1 and 2 applied.
	legacyForkFirstSchemaVersion = 998
	legacyForkLastSchemaVersion  = 999
	legacyForkBaseSchemaVersion  = 85
)

type forkMigration struct {
	version             uint
	name                string
	legacySchemaVersion uint
	fn                  customMigrationFunc
}

var forkMigrations = make(map[uint]forkMigration)

func RegisterForkMigration(version uint, name string, fn customMigrationFunc) {
	registerForkMigration(version, name, 0, fn)
}

func RegisterLegacyForkMigration(version uint, name string, legacySchemaVersion uint, fn customMigrationFunc) {
	registerForkMigration(version, name, legacySchemaVersion, fn)
}

func registerForkMigration(version uint, name string, legacySchemaVersion uint, fn customMigrationFunc) {
	if version == 0 {
		panic("fork migration version must be greater than zero")
	}
	if name == "" {
		panic("fork migration name must be non-empty")
	}
	if fn == nil {
		panic("fork migration function must be non-nil")
	}
	if _, exists := forkMigrations[version]; exists {
		panic(fmt.Sprintf("fork migration version %d already registered", version))
	}

	forkMigrations[version] = forkMigration{
		version:             version,
		name:                name,
		legacySchemaVersion: legacySchemaVersion,
		fn:                  fn,
	}
}

func getForkMigrations() []forkMigration {
	ret := make([]forkMigration, 0, len(forkMigrations))
	for _, migration := range forkMigrations {
		ret = append(ret, migration)
	}

	sort.Slice(ret, func(i, j int) bool {
		return ret[i].version < ret[j].version
	})

	return ret
}

func GetRequiredForkSchemaVersion() uint {
	migrations := getForkMigrations()
	if len(migrations) == 0 {
		return 0
	}

	return migrations[len(migrations)-1].version
}

func LegacyForkSchemaState(schemaVersion uint) (upstreamSchemaVersion uint, forkSchemaVersion uint, isLegacy bool, err error) {
	return legacyForkSchemaState(schemaVersion)
}

func (m *Migrator) CurrentForkSchemaVersion(ctx context.Context) (uint, error) {
	exists, err := m.forkMigrationTableExists(ctx)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, nil
	}

	var version sql.NullInt64
	if err := m.conn.QueryRowContext(ctx, fmt.Sprintf("SELECT MAX(version) FROM `%s`", forkSchemaMigrationsTable)).Scan(&version); err != nil {
		return 0, err
	}
	if !version.Valid {
		return 0, nil
	}

	return uint(version.Int64), nil
}

func (m *Migrator) GetNextForkMigrationVersion(current uint) uint {
	for _, migration := range getForkMigrations() {
		if migration.version > current {
			return migration.version
		}
	}

	return current
}

func (m *Migrator) RunForkMigration(ctx context.Context, newVersion uint) error {
	current, err := m.CurrentForkSchemaVersion(ctx)
	if err != nil {
		return err
	}

	expectedNext := m.GetNextForkMigrationVersion(current)
	if newVersion != expectedNext {
		return fmt.Errorf("invalid fork migration version %d, expected %d", newVersion, expectedNext)
	}

	migration, ok := forkMigrations[newVersion]
	if !ok {
		return fmt.Errorf("fork migration version %d is not registered", newVersion)
	}

	logger.Infof("Running fork database migration %d: %s", migration.version, migration.name)
	if err := m.runCustomMigration(ctx, migration.fn); err != nil {
		return fmt.Errorf("running fork migration %d (%s): %w", migration.version, migration.name, err)
	}

	if err := m.markForkMigrationApplied(ctx, migration); err != nil {
		return err
	}
	m.db.forkSchemaVersion = migration.version

	return nil
}

func (m *Migrator) RunAllForkMigrations(ctx context.Context) error {
	requiredVersion := m.RequiredForkSchemaVersion()

	for {
		currentVersion, err := m.CurrentForkSchemaVersion(ctx)
		if err != nil {
			return err
		}
		if currentVersion >= requiredVersion {
			break
		}

		nextVersion := m.GetNextForkMigrationVersion(currentVersion)
		if nextVersion == currentVersion || nextVersion > requiredVersion {
			break
		}

		if err := m.RunForkMigration(ctx, nextVersion); err != nil {
			return err
		}
	}

	return nil
}

func (m *Migrator) AdoptLegacyForkSchemaVersion(ctx context.Context, schemaVersion uint) (uint, error) {
	upstreamSchemaVersion, _, isLegacy, err := legacyForkSchemaState(schemaVersion)
	if err != nil {
		return 0, err
	}
	if !isLegacy {
		return schemaVersion, nil
	}

	logger.Infof("Adopting legacy fork schema version %d into fork migration metadata", schemaVersion)
	for _, migration := range getForkMigrations() {
		if migration.legacySchemaVersion != 0 && migration.legacySchemaVersion <= schemaVersion {
			if err := m.markForkMigrationApplied(ctx, migration); err != nil {
				return 0, err
			}
		}
	}

	if _, err := m.conn.ExecContext(ctx, "DELETE FROM `schema_migrations`"); err != nil {
		return 0, fmt.Errorf("clearing legacy schema migration version: %w", err)
	}
	if _, err := m.conn.ExecContext(ctx, "INSERT INTO `schema_migrations` (`version`, `dirty`) VALUES (?, ?)", upstreamSchemaVersion, false); err != nil {
		return 0, fmt.Errorf("resetting upstream schema migration version: %w", err)
	}

	m.db.schemaVersion = upstreamSchemaVersion
	m.db.legacyForkSchemaVersion = 0
	return upstreamSchemaVersion, nil
}

func legacyForkSchemaState(schemaVersion uint) (upstreamSchemaVersion uint, forkSchemaVersion uint, isLegacy bool, err error) {
	if schemaVersion < legacyForkFirstSchemaVersion {
		return 0, 0, false, nil
	}
	if schemaVersion > legacyForkLastSchemaVersion {
		return 0, 0, true, fmt.Errorf("cannot adopt unknown legacy fork schema version %d", schemaVersion)
	}

	for _, migration := range getForkMigrations() {
		if migration.legacySchemaVersion != 0 && migration.legacySchemaVersion <= schemaVersion && migration.version > forkSchemaVersion {
			forkSchemaVersion = migration.version
		}
	}

	return legacyForkBaseSchemaVersion, forkSchemaVersion, true, nil
}

func (m *Migrator) ensureForkMigrationTable(ctx context.Context) error {
	_, err := m.conn.ExecContext(ctx, fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
  version INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
)`, forkSchemaMigrationsTable))
	if err != nil {
		return fmt.Errorf("creating fork schema migrations table: %w", err)
	}

	return nil
}

func (m *Migrator) forkMigrationTableExists(ctx context.Context) (bool, error) {
	var name string
	err := m.conn.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", forkSchemaMigrationsTable).Scan(&name)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking fork schema migrations table: %w", err)
	}

	return true, nil
}

func (m *Migrator) markForkMigrationApplied(ctx context.Context, migration forkMigration) error {
	if err := m.ensureForkMigrationTable(ctx); err != nil {
		return err
	}

	if _, err := m.conn.ExecContext(ctx, fmt.Sprintf("DELETE FROM `%s` WHERE `version` = ?", forkSchemaMigrationsTable), migration.version); err != nil {
		return fmt.Errorf("clearing fork migration %d metadata: %w", migration.version, err)
	}
	if _, err := m.conn.ExecContext(ctx, fmt.Sprintf("INSERT INTO `%s` (`version`, `name`) VALUES (?, ?)", forkSchemaMigrationsTable), migration.version, migration.name); err != nil {
		return fmt.Errorf("recording fork migration %d metadata: %w", migration.version, err)
	}

	if migration.version > m.db.forkSchemaVersion {
		m.db.forkSchemaVersion = migration.version
	}

	return nil
}
