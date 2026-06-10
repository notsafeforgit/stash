package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/stashapp/stash/internal/manager/config"
	"github.com/stashapp/stash/pkg/fsutil"
	"github.com/stashapp/stash/pkg/job"
	"github.com/stashapp/stash/pkg/logger"
	"github.com/stashapp/stash/pkg/sqlite"
)

type migrateJobConfig interface {
	GetBackupDirectoryPath() string
	GetBackupDirectoryPathOrDefault() string
}

type MigrateJob struct {
	BackupPath string
	Config     migrateJobConfig
	Database   *sqlite.Database
}

type databaseSchemaInfo struct {
	CurrentSchemaVersion      uint
	RequiredSchemaVersion     uint
	CurrentForkSchemaVersion  uint
	RequiredForkSchemaVersion uint
	LegacyForkSchemaVersion   uint
	StepsRequired             uint
}

// PreExecute validates the environment before executing the migration.
// It returns an error if the migration cannot be performed.
func (s *MigrateJob) PreExecute() error {
	// ensure backup directory exists and is writable
	backupDir := s.Config.GetBackupDirectoryPathOrDefault()
	if backupDir != "" {
		if err := fsutil.EnsureDir(backupDir); err != nil {
			logger.Errorf("error ensuring backup directory exists: %s", err)
			logger.Warnf("Backup directory (%s) must be modified to a valid directory or removed from the config file", config.BackupDirectoryPath)
			return fmt.Errorf("error creating backup directory: %w", err)
		}
	}
	return nil
}

func (s *MigrateJob) Execute(ctx context.Context, progress *job.Progress) error {
	schemaInfo, err := s.required()
	if err != nil {
		return err
	}

	if schemaInfo.StepsRequired == 0 {
		logger.Infof("database is already at the latest schema version")
		return nil
	}

	logger.Infof(
		"Migrating database from upstream schema %d to %d and fork schema %d to %d",
		schemaInfo.CurrentSchemaVersion,
		schemaInfo.RequiredSchemaVersion,
		schemaInfo.CurrentForkSchemaVersion,
		schemaInfo.RequiredForkSchemaVersion,
	)

	// set the number of tasks = backup + required steps + optimise
	progress.SetTotal(int(schemaInfo.StepsRequired + 2))

	database := s.Database

	// always backup so that we can roll back to the previous version if
	// migration fails
	backupPath := s.BackupPath
	if backupPath == "" {
		backupPath = database.DatabaseBackupPath(s.Config.GetBackupDirectoryPath())
	} else {
		// check if backup path is a filename or path
		// filename goes into backup directory, path is kept as is
		filename := filepath.Base(backupPath)
		if backupPath == filename {
			backupPath = filepath.Join(s.Config.GetBackupDirectoryPathOrDefault(), filename)
		}
	}

	progress.ExecuteTask("Backing up database", func() {
		defer progress.Increment()

		// perform database backup
		err = database.Backup(backupPath)
	})

	if err != nil {
		return fmt.Errorf("error backing up database: %s", err)
	}

	err = s.runMigrations(ctx, progress)

	if err != nil {
		errStr := fmt.Sprintf("error performing migration: %s", err)

		// roll back to the backed up version
		restoreErr := database.RestoreFromBackup(backupPath)
		if restoreErr != nil {
			errStr = fmt.Sprintf("ERROR: unable to restore database from backup after migration failure: %s\n%s", restoreErr.Error(), errStr)
		} else {
			errStr = "An error occurred migrating the database to the latest schema version. The backup database file was automatically renamed to restore the database.\n" + errStr
		}

		return errors.New(errStr)
	}

	// if no backup path was provided, then delete the created backup
	if s.BackupPath == "" {
		if err := os.Remove(backupPath); err != nil {
			logger.Warnf("error removing unwanted database backup (%s): %s", backupPath, err.Error())
		}
	}

	// reinitialise the database
	if err := database.ReInitialise(); err != nil {
		return fmt.Errorf("error reinitialising database: %s", err)
	}

	logger.Infof("Database migration complete")

	return nil
}

func (s *MigrateJob) required() (ret databaseSchemaInfo, err error) {
	database := s.Database

	m, err := sqlite.NewMigrator(database)
	if err != nil {
		return
	}

	defer m.Close()

	ret.CurrentSchemaVersion = m.CurrentSchemaVersion()
	ret.RequiredSchemaVersion = m.RequiredSchemaVersion()
	ret.CurrentForkSchemaVersion, err = m.CurrentForkSchemaVersion(context.Background())
	if err != nil {
		return
	}
	ret.RequiredForkSchemaVersion = m.RequiredForkSchemaVersion()

	if upstreamVersion, forkVersion, isLegacy, err := sqlite.LegacyForkSchemaState(ret.CurrentSchemaVersion); err != nil {
		return ret, err
	} else if isLegacy {
		ret.LegacyForkSchemaVersion = ret.CurrentSchemaVersion
		ret.CurrentSchemaVersion = upstreamVersion
		ret.CurrentForkSchemaVersion = forkVersion
	}

	if ret.CurrentSchemaVersion > ret.RequiredSchemaVersion {
		err = fmt.Errorf("database schema version %d is newer than required schema version %d", ret.CurrentSchemaVersion, ret.RequiredSchemaVersion)
		return
	}

	// count upstream steps
	current := ret.CurrentSchemaVersion
	var steps uint
	if ret.LegacyForkSchemaVersion != 0 {
		steps++
	}
	for {
		next := m.GetNextMigrationVersion(current)
		if next == current || next > ret.RequiredSchemaVersion {
			break
		}
		steps++
		current = next
	}

	// count fork steps
	current = ret.CurrentForkSchemaVersion
	for {
		next := m.GetNextForkMigrationVersion(current)
		if next == current || next > ret.RequiredForkSchemaVersion {
			break
		}
		steps++
		current = next
	}
	ret.StepsRequired = steps

	return
}

func (s *MigrateJob) runMigrations(ctx context.Context, progress *job.Progress) error {
	database := s.Database

	m, err := sqlite.NewMigrator(database)
	if err != nil {
		return err
	}

	defer m.Close()

	logger.Info("Running migrations")

	currentSchemaVersion := m.CurrentSchemaVersion()
	if _, _, isLegacy, err := sqlite.LegacyForkSchemaState(currentSchemaVersion); err != nil {
		return err
	} else if isLegacy {
		var migrationErr error
		progress.ExecuteTask(fmt.Sprintf("Adopting legacy fork schema version %d", currentSchemaVersion), func() {
			_, migrationErr = m.AdoptLegacyForkSchemaVersion(ctx, currentSchemaVersion)
		})

		if migrationErr != nil {
			return fmt.Errorf("error adopting legacy fork schema %d: %s", currentSchemaVersion, migrationErr)
		}

		progress.Increment()
	}

	for {
		currentSchemaVersion := m.CurrentSchemaVersion()
		targetSchemaVersion := m.RequiredSchemaVersion()

		if currentSchemaVersion >= targetSchemaVersion {
			break
		}

		nextVersion := m.GetNextMigrationVersion(currentSchemaVersion)
		if nextVersion == currentSchemaVersion || nextVersion > targetSchemaVersion {
			break
		}

		var err error
		progress.ExecuteTask(fmt.Sprintf("Migrating database to schema version %d", nextVersion), func() {
			err = m.RunMigration(ctx, nextVersion)
		})

		if err != nil {
			return fmt.Errorf("error running migration for schema %d: %s", nextVersion, err)
		}

		progress.Increment()
	}

	for {
		currentSchemaVersion, err := m.CurrentForkSchemaVersion(ctx)
		if err != nil {
			return err
		}
		targetSchemaVersion := m.RequiredForkSchemaVersion()

		if currentSchemaVersion >= targetSchemaVersion {
			break
		}

		nextVersion := m.GetNextForkMigrationVersion(currentSchemaVersion)
		if nextVersion == currentSchemaVersion || nextVersion > targetSchemaVersion {
			break
		}

		var migrationErr error
		progress.ExecuteTask(fmt.Sprintf("Migrating database to fork schema version %d", nextVersion), func() {
			migrationErr = m.RunForkMigration(ctx, nextVersion)
		})

		if migrationErr != nil {
			return fmt.Errorf("error running fork migration for schema %d: %s", nextVersion, migrationErr)
		}

		progress.Increment()
	}

	// perform post-migrate analyze using the migrator connection
	progress.ExecuteTask("Optimising database", func() {
		err = m.PostMigrate(ctx)
		progress.Increment()
	})

	if err != nil {
		return fmt.Errorf("error optimising database: %s", err)
	}

	return nil
}
