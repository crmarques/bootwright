package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	cliout "github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/preflight"
	"github.com/crmarques/bootwright/internal/state/graph"
	stateview "github.com/crmarques/bootwright/internal/state/view"
	"github.com/crmarques/bootwright/internal/status"
)

const (
	installerFreshnessFresh   = status.InstallerFreshnessFresh
	installerFreshnessStale   = status.InstallerFreshnessStale
	installerFreshnessMissing = status.InstallerFreshnessMissing
	installerFreshnessUnknown = status.InstallerFreshnessUnknown
)

func newStatusCmd(stdout io.Writer) *cobra.Command {
	var output string
	watch := false
	watchInterval := 5 * time.Second
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show where the context stands and the next command",
		Long: "Inspects the current context rendered-dir and secrets-dir, surfaces declared\n" +
			"Environment, Provider, Infrastructure, ContainerCluster, and StorageCluster\n" +
			"counts, reports which clusters have fresh, stale, missing, or unknown installer\n" +
			"assets, and recommends the next command. Read-only.",
		Args: cobra.NoArgs,
		Example: `  # Show current context state and next-step hints
  bootwright status

  # Machine-readable output for CI
  bootwright status --output json`,
	}
	cf := addCommonFlags()
	addOutputFlag(cmd, &output)
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
		{Key: "input-dir", Value: ctx.InputDir},
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
	stateLoaded := loadErr == nil && status.HasAnyState(state)
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
		printClusterStatus(p, state, ctx.RenderedDir, ctx.ClustersDir, ctx.OwnershipDir, ctx.Name)
		printSharedStatus(p, state)
	}

	ledger, ledgerFound, ledgerErr := workflow.LoadRunLedger(ctx.RunsDir)
	var displays map[string]clusterDisplay
	if stateLoaded {
		displays = buildClusterDisplays(state)
	}
	printApplyLedgerStatus(p, ctx.RunsDir, displays, ledger, ledgerFound, ledgerErr)

	p.Section("Next steps")
	var items []cliout.Item
	hints := nextStepHints(stateLoaded, state, ctx.RenderedDir, ctx.ClustersDir, ctx.Name, ctx.SecretsDir, ctx.RunsDir)
	if ledgerFound && ledgerErr == nil {
		activity, _ := workflow.AssessRunActivity(ctx.RunsDir, ledger, time.Now())
		hints = status.LedgerNextSteps(ledger, activity, hints)
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
	tty := cliout.Interactive(stdout)
	first := true
	for {
		cliout.Write(stdout, statusWatchRefreshPreamble(tty, first, time.Now()))
		first = false
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

func statusWatchRefreshPreamble(tty, first bool, now time.Time) string {
	switch {
	case tty:
		return "\x1b[H\x1b[2J"
	case first:
		return ""
	default:
		return fmt.Sprintf("\n----- status refresh %s -----\n", now.Format(time.RFC3339))
	}
}

func printClusterStatus(p *cliout.Printer, state v1alpha1.State, renderedDir, clustersDir, ownershipDir, contextName string) {
	if len(state.ContainerClusters) == 0 && len(state.StorageClusters) == 0 {
		return
	}
	p.Section("Clusters")
	displays := buildClusterDisplays(state)
	partialStorage, partialErr := converge.PartiallyDestroyedStorageClusters(ownershipDir, contextName)
	if partialErr != nil {
		p.Status(cliout.StatusWarn, "teardown", "could not read partial-destroy markers under "+ownershipDir+": "+partialErr.Error()+"; partially destroyed storage clusters may not be flagged")
	}
	freshness := status.LoadEffectiveStateFreshness(state, renderedDir)
	addons := status.BuildAddons(state, clustersDir)
	containers := map[string]v1alpha1.ContainerCluster{}
	storages := map[string]v1alpha1.StorageCluster{}
	allNames := make([]string, 0, len(state.ContainerClusters)+len(state.StorageClusters))
	for _, ocp := range state.ContainerClusters {
		containers[ocp.Metadata.Name] = ocp
		allNames = append(allNames, ocp.Metadata.Name)
	}
	for _, sc := range state.StorageClusters {
		storages[sc.Metadata.Name] = sc
		allNames = append(allNames, sc.Metadata.Name)
	}
	for _, name := range orderClusterNames(allNames, displays) {
		if sc, ok := storages[name]; ok {
			p.Status(cliout.StatusInfo, name, storageStatusDetail(sc))
			if skipped, partial := partialStorage[name]; partial {
				detail := "partially destroyed: unreachable nodes skipped, devices not wiped; re-run destroy or wipe manually"
				if strings.TrimSpace(skipped) != "" {
					detail = "partially destroyed: nodes " + skipped + " skipped (unreachable), devices not wiped; re-run destroy or wipe manually"
				}
				p.Status(cliout.StatusWarn, "teardown", detail)
			}
			continue
		}
		ocp := containers[name]
		sub := stateview.ContainerClusterSubstrate(state, ocp)
		detail := fmt.Sprintf("%s  installMode=%s install=%s",
			containerDescriptor(v1alpha1.DistributionType(ocp), sub), v1alpha1.InstallMode(ocp), ocp.Spec.Install.Method)
		p.Status(cliout.StatusInfo, name, detail)
		installer := status.InstallerInstallConfigPath(clustersDir, name)
		result := status.FreshnessForInstaller(freshness, installer)
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
			detail := "no add-on record"
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
			p.Status(status, "add-on "+extension.Name, detail)
		}
	}
	orphanPartial := make([]string, 0, len(partialStorage))
	for name := range partialStorage {
		if _, ok := storages[name]; ok {
			continue
		}
		if _, ok := containers[name]; ok {
			continue
		}
		orphanPartial = append(orphanPartial, name)
	}
	sort.Strings(orphanPartial)
	for _, name := range orphanPartial {
		detail := "partially destroyed and no longer declared: unreachable nodes were skipped, devices not wiped; restore the StorageCluster declaration and re-run destroy, or wipe the nodes manually"
		if skipped := strings.TrimSpace(partialStorage[name]); skipped != "" {
			detail = "partially destroyed and no longer declared: nodes " + skipped + " skipped (unreachable), devices not wiped; restore the StorageCluster declaration and re-run destroy, or wipe the nodes manually"
		}
		p.Status(cliout.StatusWarn, name, detail)
	}
}

func storageStatusDetail(sc v1alpha1.StorageCluster) string {
	management := sc.Spec.Management
	if management == "" {
		management = v1alpha1.StorageClusterManagementManaged
	}
	return fmt.Sprintf("%s  management=%s", storageDescriptor(sc), management)
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
		if g.Kind == v1alpha1.ComponentSlotArtifactServer {
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

func nextStepHints(stateLoaded bool, state v1alpha1.State, renderedDir string, clustersDir string, contextName string, secretsDir string, runsDir string) []string {
	var secretHints []string
	needsHostTrust := false
	if stateLoaded {
		secretHints = secretNextStepHints(state, contextName, secretsDir)
		needsHostTrust = preflight.NeedsHostTrust(state, secretsDir)
	}
	applied := workflow.HasConvergeSafetyRecords(runsDir)
	return status.NextStepHints(stateLoaded, state, renderedDir, clustersDir, secretHints, needsHostTrust, applied)
}

func secretNextStepHints(state v1alpha1.State, contextName, secretsDir string) []string {
	entries, err := declaredSecretEntriesForContext(contextName, secretsDir, state)
	return status.SecretNextStepHints(state, entries, err)
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
	return status.InstallerInstallConfigPath(clustersDir, clusterName)
}
