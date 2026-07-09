package catalog

import "embed"

//go:embed catalog.yaml openshift-data-foundation fusion-data-foundation
var Content embed.FS
