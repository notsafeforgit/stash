package fsutil

// PersistentFileIdentityPrefix distinguishes filesystem IDs from the historical
// Unix device-number identities. Never reinterpret a saved device number as a
// filesystem ID: existing reviewed bindings must keep their original checks.
const PersistentFileIdentityPrefix = "fsid-v1:"
