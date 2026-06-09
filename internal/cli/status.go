package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/render"
	"github.com/crmarques/bootwright/internal/state/graph"
)

const (
	outputText = "text"
	outputJSON = "json"
)

func validateOutputFormat(value string) error {
	if value != outputText && value != outputJSON {
		return fmt.Errorf("--output must be %q or %q", outputText, outputJSON)
	}
	return nil
}

func newStatusCmd(stdout io.Writer) *cobra.Command {
	output := outputText
	watch := false
	watchInterval := 5 * time.Second
	cmd := &cobra.Command{
		Use:   "status [target]",
		Short: "Report context state and the suggested next command",
		Long: "Inspects the current context rendered-dir and secrets-dir, surfaces declared\n" +
			"Environment/Provider/Infrastructure/ContainerCluster counts, reports which\n" +
			"clusters have fresh, stale, missing, or unknown installer assets, and recommends the next\n" +
			"command. Read-only.",
		Args: cobra.NoArgs,
		Example: `  # Show current context state and next-step hints
  bootwright status

  # Machine-readable output for CI
  bootwright status --output json`,
	}
	cf := addCommonFlags()
	cmd.Flags().StringVar(&output, "output", output, "output format: text|json")
	cmd.Flags().BoolVar(&watch, "watch", false, "refresh status until the current apply run reaches a terminal state")
	cmd.Flags().DurationVar(&watchInterval, "watch-interval", watchInterval, "status refresh interval for --watch")
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		if err := validateOutputFormat(output); err != nil {
			return failErr(2, err)
		}
		if watch && output == outputJSON {
			return failErr(2, fmt.Errorf("--watch is only supported with text output"))
		}
		if output == outputJSON {
			return runStatusJSON(stdout, cf)
		}
		if watch {
			return runStatusWatch(c.Context(), stdout, cf, watchInterval)
		}
		return runStatus(stdout, cf)
	}
	return cmd
}

func runStatus(stdout io.Writer, cf *commonFlags) error {
	ctx, err := cf.resolve()
	if err != nil {
		return runStatusSetup(stdout, err)
	}
	p := cliout.New(stdout)
	p.Command("status")

	p.Section("Context")
	p.Fields([]cliout.Field{
		{Key: "context", Value: ctx.Name},
		{Key: "context-dir", Value: ctx.BaseDir},
		{Key: "workspace", Value: ctx.InputDir},
		{Key: "rendered-dir", Value: ctx.RenderedDir},
		{Key: "clusters-dir", Value: ctx.ClustersDir},
		{Key: "runs-dir", Value: ctx.RunsDir},
		{Key: "managed-services-dir", Value: ctx.ManagedServicesDir},
		{Key: "provider-state-dir", Value: ctx.ProviderStateDir},
		{Key: "ownership-dir", Value: ctx.OwnershipDir},
		{Key: "secrets-dir", Value: ctx.SecretsDir},
	})
	p.Checks(contextReadinessChecks(ctx))

	state, loadErr := loadOptionalDesiredState(cf)
	stateLoaded := loadErr == nil && hasAnyState(state)
	source := stateSource(cf)

	p.Section("Desired state")
	fields := []cliout.Field{{Key: "source", Value: source}}
	switch {
	case loadErr != nil:
		p.Fields(fields)
		p.Status(cliout.StatusFail, "load", loadErr.Error())
	case !stateLoaded:
		p.Fields(fields)
		p.Status(cliout.StatusFail, "load", "no desired state found in the context workspace")
	default:
		fields = append(fields, stateCountFields(state)...)
		p.Fields(fields)
		p.Status(cliout.StatusOK, "load", "workspace input loads, normalizes, and validates")
	}

	if stateLoaded {
		printSecretStatus(p, ctx.Name, ctx.SecretsDir, state)
		p.Checks(append(contextHostTrustChecks(ctx.BaseDir, state), bastionLocalityCheck(state)))
		printClusterStatus(p, state, ctx.RenderedDir, ctx.ClustersDir)
		printSharedStatus(p, state)
	}

	ledger, ledgerFound, ledgerErr := workflow.LoadRunLedger(ctx.RunsDir)
	printApplyLedgerStatus(p, ctx.RunsDir, ctx.ClustersDir, ledger, ledgerFound, ledgerErr)

	p.Section("Next steps")
	var items []cliout.Item
	hints := nextStepHints(stateLoaded, state, ctx.RenderedDir, ctx.ClustersDir, ctx.Name, ctx.SecretsDir)
	if ledgerFound && ledgerErr == nil {
		activity, _ := workflow.AssessRunActivity(ctx.RunsDir, ledger, time.Now())
		hints = ledgerNextSteps(ledger, activity, hints)
	}
	if ledgerErr != nil {
		hints = append([]string{"inspect apply ledger: " + ledgerErr.Error()}, hints...)
	}
	for _, hint := range hints {
		items = append(items, cliout.Item{Label: hint})
	}
	p.List(items)
	return nil
}

func runStatusWatch(ctx context.Context, stdout io.Writer, cf *commonFlags, interval time.Duration) error {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	for {
		if err := runStatus(stdout, cf); err != nil {
			return err
		}
		contextState, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		ledger, found, err := workflow.LoadRunLedger(contextState.RunsDir)
		if err != nil || !found || ledger.Terminal() {
			return nil
		}
		activity, err := workflow.AssessRunActivity(contextState.RunsDir, ledger, time.Now())
		if err != nil || activity.State == workflow.RunActivityStale {
			return err
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func printClusterStatus(p *cliout.Printer, state v1alpha1.State, renderedDir, clustersDir string) {
	if len(state.ContainerClusters) == 0 {
		return
	}
	p.Section("Clusters")
	freshness := loadEffectiveStateFreshness(state, renderedDir)
	names := make([]string, 0, len(state.ContainerClusters))
	byName := map[string]v1alpha1.ContainerCluster{}
	for _, ocp := range state.ContainerClusters {
		names = append(names, ocp.Metadata.Name)
		byName[ocp.Metadata.Name] = ocp
	}
	sort.Strings(names)
	addons := buildStatusAddons(state, clustersDir)
	for _, name := range names {
		ocp := byName[name]
		detail := fmt.Sprintf("installMode=%s install=%s", v1alpha1.InstallMode(ocp), ocp.Spec.Install.Method)
		p.Status(cliout.StatusOK, name, detail)
		installer := installerInstallConfigPath(clustersDir, name)
		result := freshnessForInstaller(freshness, installer)
		switch result.State {
		case installerFreshnessFresh:
			p.Status(cliout.StatusOK, "installer", installer+" (fresh)")
		case installerFreshnessStale:
			p.Status(cliout.StatusFail, "installer", installer+" (stale; current desired state differs from effective-state.yaml)")
		case installerFreshnessUnknown:
			p.Status(cliout.StatusWarn, "installer", installer+" (freshness unknown; "+result.Error+")")
		default:
			p.Status(cliout.StatusFail, "installer", "not rendered")
		}
		for _, extension := range addons[name] {
			status := cliout.StatusSkip
			detail := "no addon record"
			if extension.Status != "" {
				status = cliout.StatusOK
				if extension.Status != string(extensionrecords.RecordStatusReady) {
					status = cliout.StatusWarn
				}
				detail = extension.Status
				if extension.Phase != "" {
					detail += " phase=" + extension.Phase
				}
			}
			p.Status(status, "addon "+extension.Name, detail)
		}
	}
}

func printSharedStatus(p *cliout.Printer, state v1alpha1.State) {
	groups := stategraph.ResolveMachineServices(state).SharedServices()
	if len(groups) == 0 {
		return
	}
	p.Section("Shared services")
	for _, g := range groups {
		machine := g.MachineRef
		if machine == "" {
			machine = "<unresolved>"
		}
		detail := fmt.Sprintf("on %s, used by %s", machine, joinNames(g.ConsumingClusters))
		if g.Kind == v1alpha1.ComponentSlotArtifacts {
			detail += " (environment artifact server)"
		}
		p.Status(cliout.StatusOK, fmt.Sprintf("%s/%s/%s", g.ProviderName, g.Kind, g.CapabilityName), detail)
	}
}

func printSecretStatus(p *cliout.Printer, contextName, secretsDir string, state v1alpha1.State) {
	entries, err := declaredSecretEntriesForContext(contextName, secretsDir, state)
	if err != nil {
		p.Section("Secret material")
		p.Status(cliout.StatusFail, "declared secrets", err.Error())
		return
	}
	if len(entries) == 0 {
		return
	}
	p.Section("Secret material")
	for _, entry := range entries {
		status := cliout.StatusOK
		detail := entry.Type + " " + strings.Join(entry.Paths, ", ")
		if !entry.Present {
			status = cliout.StatusFail
			if entry.Detail != "" {
				detail = entry.Detail
			}
		}
		p.Status(status, entry.Name, detail)
	}
}

func nextStepHints(stateLoaded bool, state v1alpha1.State, renderedDir string, clustersDir string, contextName string, secretsDir string) []string {
	if stateLoaded {
		hints := []string{"bootwright secret list"}
		hints = append(hints, secretNextStepHints(state, contextName, secretsDir)...)
		if statusNeedsHostTrust(state, secretsDir) {
			hints = append(hints, "bootwright host trust")
		}
		hints = append(hints, "bootwright bastion setup --yes", "bootwright preflight all", "bootwright render effective")
		needsInstaller := clustersNeedingInstallerRender(state, renderedDir, clustersDir)
		if len(needsInstaller) > 0 {
			hints = append(hints, "bootwright plan")
			return hints
		}
		hints = append(hints,
			"bootwright plan",
			"bootwright apply --yes",
			"bootwright status --watch",
			"bootwright cluster access",
		)
		return hints
	}
	return []string{
		"edit desired-state YAML under the context input directory",
		"bootwright secret list",
		"bootwright preflight all",
	}
}

func secretNextStepHints(state v1alpha1.State, contextName, secretsDir string) []string {
	entries, err := declaredSecretEntriesForContext(contextName, secretsDir, state)
	if err != nil {
		return nil
	}
	generatedMissing := false
	materializedMissing := false
	var contextMissing []string
	env := primaryEnvironmentForSync(state)
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
		hints = append(hints, "bootwright secret sync")
	}
	hints = append(hints, contextSecretSetHints(contextMissing)...)
	return hints
}

// contextSecretSetHints emits a `secret set` hint for every missing
// context-local secret, not just the first, so an operator following the status
// spine sees the full set of required secrets in one read. The OpenShift pull
// secret is surfaced first because it is the most universally required and
// otherwise sorts last among alphabetically-ordered names.
func contextSecretSetHints(missing []string) []string {
	var pull, rest []string
	for _, name := range missing {
		if name == v1alpha1.DefaultPullSecretName {
			pull = append(pull, "bootwright secret set "+name+" --pull-secret <path>")
		} else {
			rest = append(rest, "bootwright secret set "+name+" --from-file <path>")
		}
	}
	return append(pull, rest...)
}

func clustersNeedingInstallerRender(state v1alpha1.State, renderedDir, clustersDir string) []string {
	freshness := loadEffectiveStateFreshness(state, renderedDir)
	var needs []string
	for _, ocp := range state.ContainerClusters {
		path := installerInstallConfigPath(clustersDir, ocp.Metadata.Name)
		switch freshnessForInstaller(freshness, path).State {
		case installerFreshnessFresh:
			continue
		default:
			needs = append(needs, ocp.Metadata.Name)
		}
	}
	sort.Strings(needs)
	return needs
}

func joinNames(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ","
		}
		out += n
	}
	return out
}

func stateSource(cf *commonFlags) string {
	if cf.ctx.InputDir != "" {
		return cf.ctx.InputDir
	}
	return "(none)"
}

func installerInstallConfigPath(clustersDir, clusterName string) string {
	return filepath.Join(clustersDir, clusterName, "rendered", render.InstallerRelativeDir, "install-config.yaml")
}

func hasAnyState(s v1alpha1.State) bool {
	return len(s.Environments)+len(s.Machines)+len(s.MachineImages)+len(s.MachineInstallProfiles)+len(s.InfraProviders)+len(s.ContainerClusters)+len(s.StorageClusters)+len(s.StoragePlacementPolicies)+len(s.StoragePools)+len(s.StorageFilesystems)+len(s.StorageObjectGateways)+len(s.StorageExports)+len(s.ClusterAddons)+len(s.ClusterAddonProfiles)+len(s.ClusterAddonBindings) > 0
}
