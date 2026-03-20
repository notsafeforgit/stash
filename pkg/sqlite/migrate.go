package sqlite

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	sqlite3mig "github.com/golang-migrate/migrate/v4/database/sqlite3"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jmoiron/sqlx"
	"github.com/stashapp/stash/pkg/logger"
)

func getAvailableMigrations() []uint {
	entries, err := migrationsBox.ReadDir("migrations")
	if err != nil {
		return nil
	}
	var versions []uint
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".up.sql") {
			parts := strings.SplitN(entry.Name(), "_", 2)
			if len(parts) == 2 {
				if v, err := strconv.ParseUint(parts[0], 10, 32); err == nil {
					versions = append(versions, uint(v))
				}
			}
		}
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	return versions
}

func GetRequiredSchemaVersion() uint {
	versions := getAvailableMigrations()
	maxVersion := appSchemaVersion
	for _, v := range versions {
		if v >= 900 && v > maxVersion {
			maxVersion = v
		}
	}
	return maxVersion
}

func (db *Database) NeedsMigration() bool {
	return db.schemaVersion != GetRequiredSchemaVersion()
}

type Migrator struct {
	db   *Database
	conn *sqlx.DB
	m    *migrate.Migrate
}

func NewMigrator(db *Database) (*Migrator, error) {
	m := &Migrator{
		db: db,
	}

	const disableForeignKeys = true
	const writable = true
	var err error
	m.conn, err = m.db.open(disableForeignKeys, writable)
	if err != nil {
		return nil, err
	}

	m.conn.SetMaxOpenConns(maxReadConnections)
	m.conn.SetMaxIdleConns(maxReadConnections)
	m.conn.SetConnMaxIdleTime(dbConnTimeout)

	m.m, err = m.getMigrate()

	// if error encountered, close the connection
	if err != nil {
		m.Close()
	}

	return m, err
}

func (m *Migrator) Close() {
	if m.m != nil {
		m.m.Close()
		m.m = nil
	}
}

func (m *Migrator) CurrentSchemaVersion() uint {
	databaseSchemaVersion, _, _ := m.m.Version()
	return databaseSchemaVersion
}

func (m *Migrator) RequiredSchemaVersion() uint {
	return GetRequiredSchemaVersion()
}

func (m *Migrator) GetNextMigrationVersion(current uint) uint {
	versions := getAvailableMigrations()
	for _, v := range versions {
		if v > current {
			return v
		}
	}
	return current
}

func (m *Migrator) getMigrate() (*migrate.Migrate, error) {
	migrations, err := iofs.New(migrationsBox, "migrations")
	if err != nil {
		return nil, err
	}

	driver, err := sqlite3mig.WithInstance(m.conn.DB, &sqlite3mig.Config{})
	if err != nil {
		return nil, err
	}

	// use sqlite3Driver so that migration has access to durationToTinyInt
	return migrate.NewWithInstance(
		"iofs",
		migrations,
		m.db.dbPath,
		driver,
	)
}

func (m *Migrator) RunMigration(ctx context.Context, newVersion uint) error {
	databaseSchemaVersion, _, _ := m.m.Version()

	expectedNext := m.GetNextMigrationVersion(databaseSchemaVersion)
	if newVersion != expectedNext {
		return fmt.Errorf("invalid migration version %d, expected %d", newVersion, expectedNext)
	}

	// run pre migrations as needed
	if err := m.runCustomMigrations(ctx, preMigrations[newVersion]); err != nil {
		return fmt.Errorf("running pre migrations for schema version %d: %w", newVersion, err)
	}

	if err := m.m.Steps(1); err != nil {
		// migration failed
		return err
	}

	// run post migrations as needed
	if err := m.runCustomMigrations(ctx, postMigrations[newVersion]); err != nil {
		return fmt.Errorf("running post migrations for schema version %d: %w", newVersion, err)
	}

	// update the schema version
	m.db.schemaVersion, _, _ = m.m.Version()

	return nil
}

func (m *Migrator) runCustomMigrations(ctx context.Context, fns []customMigrationFunc) error {
	for _, fn := range fns {
		if err := m.runCustomMigration(ctx, fn); err != nil {
			return err
		}
	}

	return nil
}

func (m *Migrator) runCustomMigration(ctx context.Context, fn customMigrationFunc) error {
	if err := fn(ctx, m.conn); err != nil {
		return err
	}

	return nil
}

func (m *Migrator) PostMigrate(ctx context.Context) error {
	// optimise the database
	var err error
	logger.Info("Running database analyze")

	// don't use Optimize/vacuum as this adds a significant amount of time
	// to the migration
	err = analyze(ctx, m.conn)

	if err == nil {
		logger.Debug("Flushing WAL")
		err = flushWAL(ctx, m.conn)
	}

	if err != nil {
		return fmt.Errorf("error optimising database: %s", err)
	}

	return nil
}

func (db *Database) getDatabaseSchemaVersion() (uint, error) {
	m, err := NewMigrator(db)
	if err != nil {
		return 0, err
	}
	defer m.Close()

	ret, _, _ := m.m.Version()
	return ret, nil
}

func (db *Database) ReInitialise() error {
	return db.initialise()
}

// RunAllMigrations runs all migrations to bring the database up to the current schema version
func (db *Database) RunAllMigrations() error {
	ctx := context.Background()

	m, err := NewMigrator(db)
	if err != nil {
		return err
	}
	defer m.Close()

	databaseSchemaVersion, _, _ := m.m.Version()
	requiredVersion := m.RequiredSchemaVersion()

	if databaseSchemaVersion < requiredVersion {
		logger.Infof("Migrating database from version %d to %d", databaseSchemaVersion, requiredVersion)

		for {
			databaseSchemaVersion, _, _ = m.m.Version()
			if databaseSchemaVersion >= requiredVersion {
				break
			}

			nextVersion := m.GetNextMigrationVersion(databaseSchemaVersion)
			if nextVersion == databaseSchemaVersion {
				break
			}

			if err := m.RunMigration(ctx, nextVersion); err != nil {
				return err
			}
		}
	}

	return nil
}
