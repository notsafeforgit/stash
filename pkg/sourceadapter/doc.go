// Package sourceadapter interprets retained metadata and profile locators from
// gallery-dl and its child extractors. It returns source claims and format
// projections, never library decisions. All operations are deterministic and
// offline; this package must not depend on archive services, repositories,
// filesystem/process access, network clients or runtime plugin registration.
//
// Core archive services validate these claims, preserve capture hashes, and
// choose associations within the same transactions as ordinary library edits.
// Adding a site here cannot grant it permission to link media, merge accounts,
// override explicit corrections, or acknowledge producer work.
package sourceadapter
