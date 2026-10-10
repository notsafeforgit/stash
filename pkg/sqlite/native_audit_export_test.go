package sqlite

// AuditForTesting exposes the read-only full domain audit to external-package
// corruption fixtures without adding an application API or opening a writer.
// Migration and snapshot tests separately exercise the production entry points.
func (db *Database) AuditForTesting(path string) error {
	return validateDatabaseLineage(path)
}
