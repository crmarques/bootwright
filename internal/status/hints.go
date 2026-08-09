package status

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/state/view"
)

const unavailableContextNextStepGuidance = "select a context explicitly before continuing; no runnable command is suggested because status could not resolve its target context"

type NextStepHint struct {
	Args     []string
	Guidance string
}

func NextStepHints(stateLoaded bool, state v1alpha1.State, renderedDir string, clustersDir string, contextName string, secretHints []NextStepHint, needsHostTrust bool, applied bool) []NextStepHint {
	if strings.TrimSpace(contextName) == "" {
		return []NextStepHint{{Guidance: unavailableContextNextStepGuidance}}
	}
	if stateLoaded {
		hints := []NextStepHint{contextCommandHint(contextName, "bootwright", "secret", "list")}
		hints = append(hints, secretHints...)
		if needsHostTrust {
			hints = append(hints, contextCommandHint(contextName, "bootwright", "machine", "trust"))
		}
		hints = append(hints,
			contextCommandHint(contextName, "bootwright", "bastion", "setup"),
			contextCommandHint(contextName, "bootwright", "preflight", "all"),
			contextCommandHint(contextName, "bootwright", "render", "effective"),
		)
		if applied {
			hints = append(hints, contextCommandHint(contextName, "bootwright", "diff"))
		}
		needsInstaller := ClustersNeedingInstallerRender(state, renderedDir, clustersDir)
		if len(needsInstaller) > 0 {
			hints = append(hints, contextCommandHint(contextName, "bootwright", "plan"))
			return hints
		}
		hints = append(hints,
			contextCommandHint(contextName, "bootwright", "plan"),
			contextCommandHint(contextName, "bootwright", "apply"),
			contextCommandHint(contextName, "bootwright", "status", "--watch"),
			contextCommandHint(contextName, "bootwright", "cluster", "info"),
		)
		return hints
	}
	return []NextStepHint{
		{Guidance: "edit desired-state YAML under the context input directory"},
		contextCommandHint(contextName, "bootwright", "secret", "list"),
		contextCommandHint(contextName, "bootwright", "preflight", "all"),
	}
}

func SecretNextStepHints(state v1alpha1.State, entries []SecretEntry, err error, contextName string) []NextStepHint {
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
	var hints []NextStepHint
	if materializedMissing || generatedMissing {
		hints = append(hints, contextCommandHint(contextName, "bootwright", "secret", "generate"))
	}
	hints = append(hints, ContextSecretSetHints(contextName, contextMissing)...)
	return hints
}

func ContextSecretSetHints(contextName string, missing []string) []NextStepHint {
	if len(missing) > 0 && strings.TrimSpace(contextName) == "" {
		return []NextStepHint{{Guidance: unavailableContextNextStepGuidance}}
	}
	var pull, rest []NextStepHint
	for _, name := range missing {
		if name == v1alpha1.DefaultPullSecretName {
			pull = append(pull, contextCommandHint(contextName, "bootwright", "secret", "set", "--name", name, "--pull-secret", "<path>"))
		} else {
			rest = append(rest, contextCommandHint(contextName, "bootwright", "secret", "set", "--name", name, "--from-file", "<path>"))
		}
	}
	return append(pull, rest...)
}

func contextCommandHint(contextName string, args ...string) NextStepHint {
	contextName = strings.TrimSpace(contextName)
	if contextName == "" {
		return NextStepHint{Guidance: unavailableContextNextStepGuidance}
	}
	command := append([]string(nil), args...)
	command = append(command, "--context", contextName)
	return NextStepHint{Args: command}
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
