package status

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/state/view"
)

func NextStepHints(stateLoaded bool, state v1alpha1.State, renderedDir string, clustersDir string, secretHints []string, needsHostTrust bool, applied bool) []string {
	if stateLoaded {
		hints := []string{"bootwright secret list"}
		hints = append(hints, secretHints...)
		if needsHostTrust {
			hints = append(hints, "bootwright machine trust")
		}
		hints = append(hints, "bootwright bastion setup --yes", "bootwright preflight all", "bootwright render effective")
		if applied {
			hints = append(hints, "bootwright diff")
		}
		needsInstaller := ClustersNeedingInstallerRender(state, renderedDir, clustersDir)
		if len(needsInstaller) > 0 {
			hints = append(hints, "bootwright plan")
			return hints
		}
		hints = append(hints,
			"bootwright plan",
			"bootwright apply --yes",
			"bootwright status --watch",
			"bootwright cluster info",
		)
		return hints
	}
	return []string{
		"edit desired-state YAML under the context input directory",
		"bootwright secret list",
		"bootwright preflight all",
	}
}

func SecretNextStepHints(state v1alpha1.State, entries []SecretEntry, err error) []string {
	if err != nil {
		return nil
	}
	generatedMissing := false
	materializedMissing := false
	var contextMissing []string
	env := stateview.Environment(state)
	for _, entry := range entries {
		if entry.Present {
			continue
		}
		if strings.HasPrefix(entry.Type, "generated:") {
			generatedMissing = true
			continue
		}
		if env != nil && env.Spec.SecretStorage.Mode == v1alpha1.SecretStorageModeContext && strings.HasPrefix(entry.Type, "file") {
			materializedMissing = true
			continue
		}
		if entry.Type == "context" {
			contextMissing = append(contextMissing, entry.Name)
		}
	}
	var hints []string
	if materializedMissing || generatedMissing {
		hints = append(hints, "bootwright secret generate")
	}
	hints = append(hints, ContextSecretSetHints(contextMissing)...)
	return hints
}

func ContextSecretSetHints(missing []string) []string {
	var pull, rest []string
	for _, name := range missing {
		if name == v1alpha1.DefaultPullSecretName {
			pull = append(pull, "bootwright secret set --name "+name+" --pull-secret <path>")
		} else {
			rest = append(rest, "bootwright secret set --name "+name+" --from-file <path>")
		}
	}
	return append(pull, rest...)
}

func ClustersNeedingInstallerRender(state v1alpha1.State, renderedDir, clustersDir string) []string {
	freshness := LoadEffectiveStateFreshness(state, renderedDir)
	var needs []string
	for _, ocp := range state.ContainerClusters {
		path := InstallerInstallConfigPath(clustersDir, ocp.Metadata.Name)
		switch FreshnessForInstaller(freshness, path).State {
		case InstallerFreshnessFresh:
			continue
		default:
			needs = append(needs, ocp.Metadata.Name)
		}
	}
	sort.Strings(needs)
	return needs
}

func InstallerInstallConfigPath(clustersDir, clusterName string) string {
	return filepath.Join(clustersDir, clusterName, "rendered", render.InstallerRelativeDir, "install-config.yaml")
}
