// Package catalog is the repo-shipped native add-on catalog. The directory
// doubles as authored content and Go package so the committed files embed
// directly into the binary with no sync step (contrast
// internal/converge/bundle, whose zip is assembled by make sync-bundle).
// internal/addons/nativecatalog is the only consumer.
package catalog

import "embed"

// Content holds catalog.yaml plus one directory per catalog entry. Adding an
// entry means adding its directory name to this pattern list;
// internal/addons/nativecatalog's consistency test fails on drift.
//
//go:embed catalog.yaml openshift-data-foundation fusion-data-foundation
var Content embed.FS
