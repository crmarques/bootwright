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
}

type EffectiveResult struct {
	Counts    Counts
	Effective api.Catalog
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
	ExcludedResourceFiles     []string                 `json:"excludedResourceFiles"`
	Advisories                []diagnostics.Diagnostic `json:"advisories"`
}
