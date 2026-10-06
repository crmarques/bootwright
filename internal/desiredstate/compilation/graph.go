package compilation

import (
	"slices"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/addons"
	"github.com/crmarques/bootwright/internal/environment"
)

type StorageAttachments func(api.Catalog) []addons.Attachment
type EnvironmentSelector func(api.Catalog, []environment.Attachment) environment.Selection

type GraphSelector struct {
	attachments StorageAttachments
	environment EnvironmentSelector
}

func NewGraphSelector(attachments StorageAttachments, selection EnvironmentSelector) GraphSelector {
	return GraphSelector{attachments: attachments, environment: selection}
}

func (g GraphSelector) Select(catalog api.Catalog) Selection {
	if g.attachments == nil || g.environment == nil {
		result := Selection{Catalog: catalog}
		for _, object := range catalog.OfKind(api.Environment) {
			result.Problems = append(result.Problems, ObjectIssue{Object: object, Issue: api.Issue{Code: "api.invariant", Message: "environment graph selection is not configured"}})
		}
		return result
	}
	attachments := []environment.Attachment{}
	for _, attachment := range g.attachments(catalog) {
		attachments = append(attachments, environment.Attachment{ClusterRef: attachment.ClusterRef, ExportRef: attachment.ExportRef})
	}
	selection := g.environment(catalog, attachments)
	result := Selection{Catalog: selection.Catalog, ExcludedContainerClusters: slices.Clone(selection.ExcludedContainerClusters), ExcludedStorageClusters: slices.Clone(selection.ExcludedStorageClusters)}
	for _, problem := range selection.Problems {
		result.Problems = append(result.Problems, ObjectIssue{Object: problem.Object, Issue: problem.Issue, Target: problem.Target})
	}
	return result
}
