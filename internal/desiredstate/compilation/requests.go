package compilation

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type ValidateRequest struct {
	ContextName string
	Files       []string
}

type EffectiveRequest struct {
	ContextName string
	Origins     bool
}

type EffectiveResult struct {
	Counts    Counts
	Effective api.Catalog
	Origins   []ObjectOrigins
}

type ObjectOrigins struct {
	Object string
	Fields []FieldOrigin
}

type Counts struct {
	FilesSeen      int `json:"filesSeen"`
	ObjectsDecoded int `json:"objectsDecoded"`
}

type Report struct {
	Diagnostics               []diagnostics.Diagnostic `json:"-"`
	Counts                    Counts                   `json:"counts"`
	ExcludedContainerClusters []string                 `json:"excludedContainerClusters"`
	ExcludedStorageClusters   []string                 `json:"excludedStorageClusters"`
	Advisories                []diagnostics.Diagnostic `json:"advisories"`
}
