package models

import "context"

type DefaultFilter struct {
	View     string
	Revision int
	Enabled  bool
	Filter   SavedFilter
}

// ConfigurationMigration checkpoints publication after native records commit.
// Only the fields being imported belong in these JSON documents, never secrets.
type ConfigurationMigration struct {
	Name       string `db:"name"`
	SourceJSON string `db:"source_json"`
	TargetJSON string `db:"target_json"`
	State      string `db:"state"`
}

type DefaultFilterConflict struct {
	View          string
	Revision      int
	MigrationName string
	Evidence      string
	Alternative   *FilterAST
	ImportError   string
	Selection     string
}

type DefaultFilterReaderWriter interface {
	All(context.Context) ([]*DefaultFilter, error)
	Find(context.Context, string) (*DefaultFilter, error)
	Set(context.Context, string, *SavedFilter) error
	Clear(context.Context, string) error
	Conflicts(context.Context) ([]*DefaultFilterConflict, error)
	CreateConflict(context.Context, *DefaultFilterConflict) error
	ResolveConflict(context.Context, string, string) error
}

type ConfigurationMigrationReaderWriter interface {
	Find(context.Context, string) (*ConfigurationMigration, error)
	Create(context.Context, *ConfigurationMigration) error
	Publish(context.Context, string) error
}
