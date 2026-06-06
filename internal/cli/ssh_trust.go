package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/infra/locality"
	"github.com/crmarques/bootwright/internal/runtime/sshtrust"
)

const defaultHostTrustScanTimeout = 5 * time.Second

type hostTrustDeps struct {
	scan     func(context.Context, string, time.Duration) ([]sshtrust.ScannedKey, error)
	lookPath func(string, []string) (string, error)
}

var defaultHostTrustDeps = hostTrustDeps{
	scan:     scanSSHHostKeys,
	lookPath: defaultLookPath,
}

func newHostCmd(stdin io.Reader, stdout io.Writer, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "host <command>",
		Short: "Manage declared durable hosts",
	}
	cmd.AddCommand(newHostTrustCmd(stdin, stdout, stderr))
	requireSubcommand(cmd)
	return cmd
}

func newHostTrustCmd(stdin io.Reader, stdout io.Writer, _ io.Writer) *cobra.Command {
	var (
		hosts   string
		replace string
		dryRun  bool
		yes     bool
		format  string
	)
	format = outputText
	cmd := &cobra.Command{
		Use:   "trust",
		Short: "Record SSH host-key trust for declared Machines",
		Args:  cobra.NoArgs,
		Example: `  bootwright host trust
  bootwright host trust --hosts provider-01,ceph-dc1-0
  bootwright host trust --replace provider-01
  bootwright host trust --dry-run
  bootwright host trust --output json --dry-run`,
	}
	cmd.Flags().StringVar(&hosts, "hosts", "", "comma-separated Machine names to trust")
	cmd.Flags().StringVar(&replace, "replace", "", "comma-separated Machine names whose changed trust may be replaced")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "scan and report trust changes without writing context trust files")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompts")
	cmd.Flags().StringVar(&format, "output", format, "output format: text|json")
	cf := addCommonFlags()
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		if err := validateOutputFormat(format); err != nil {
			return failErr(2, err)
		}
		ctx, err := cf.resolve()
		if err != nil {
			return failErr(1, err)
		}
		state, err := loadDesiredState(cf)
		if err != nil {
			return failErr(1, err)
		}
		report, err := runHostTrust(c.Context(), stdin, stdout, ctx.Name, ctx.BaseDir, state, hostTrustOptions{
			Hosts:   hosts,
			Replace: replace,
			DryRun:  dryRun,
			Yes:     yes,
			Output:  format,
		}, defaultHostTrustDeps)
		if err != nil {
			return failErr(1, err)
		}
		if format == outputJSON {
			return output.JSON(stdout, report)
		}
		return nil
	}
	return cmd
}

type hostTrustOptions struct {
	Hosts   string
	Replace string
	DryRun  bool
	Yes     bool
	Output  string
}

type hostTrustReport struct {
	OK             bool                  `json:"ok"`
	Context        string                `json:"context"`
	StorePath      string                `json:"storePath"`
	KnownHostsPath string                `json:"knownHostsPath"`
	DryRun         bool                  `json:"dryRun"`
	Hosts          []hostTrustHostReport `json:"hosts"`
}

type hostTrustHostReport struct {
	Name                      string `json:"name"`
	Address                   string `json:"address,omitempty"`
	Action                    string `json:"action"`
	KeyType                   string `json:"keyType,omitempty"`
	FingerprintSHA256         string `json:"fingerprintSHA256,omitempty"`
	PreviousAddress           string `json:"previousAddress,omitempty"`
	PreviousFingerprintSHA256 string `json:"previousFingerprintSHA256,omitempty"`
	Reason                    string `json:"reason,omitempty"`
}

func runHostTrust(ctx context.Context, stdin io.Reader, stdout io.Writer, contextName, contextDir string, state v1alpha1.State, opts hostTrustOptions, deps hostTrustDeps) (hostTrustReport, error) {
	if deps.scan == nil {
		deps.scan = scanSSHHostKeys
	}
	if deps.lookPath == nil {
		deps.lookPath = defaultLookPath
	}
	selected, err := parseNameSet(opts.Hosts)
	if err != nil {
		return hostTrustReport{}, err
	}
	replace, err := parseNameSet(opts.Replace)
	if err != nil {
		return hostTrustReport{}, err
	}
	report := hostTrustReport{
		OK:             true,
		Context:        contextName,
		StorePath:      sshtrust.StorePathForContext(contextDir),
		KnownHostsPath: sshtrust.KnownHostsPathForContext(contextDir),
		DryRun:         opts.DryRun,
	}
	store, err := sshtrust.Load(report.StorePath)
	if err != nil {
		return report, err
	}
	if hostTrustNeedsScan(state, selected) {
		if _, err := deps.lookPath("ssh-keyscan", nil); err != nil {
			return report, fmt.Errorf("ssh-keyscan is required to record SSH host trust: %w", err)
		}
	}
	plan, err := buildHostTrustPlan(ctx, state, store, selected, replace, deps.scan)
	if err != nil {
		return report, err
	}
	report.Hosts = plan.reports
	if opts.Output == outputJSON && !opts.DryRun && !opts.Yes && plan.pendingWrites > 0 {
		return report, errors.New("host trust has pending changes; rerun with --yes or use --dry-run with --output json")
	}
	if opts.Output == outputText {
		printHostTrustPlan(stdout, report)
	}
	if opts.DryRun {
		return report, nil
	}
	if plan.pendingWrites > 0 && !opts.Yes && !confirm(stdin, stdout, fmt.Sprintf("Trust %d SSH host key change(s)? [y/N] (default: no): ", plan.pendingWrites)) {
		return report, errors.New("host trust aborted")
	}
	for _, record := range plan.records {
		store.Upsert(record)
	}
	if len(plan.records) > 0 || len(managedTrustMachines(state, controllerLocalityPolicy)) > 0 {
		if err := sshtrust.Save(sshtrust.DirForContext(contextDir), store); err != nil {
			return report, err
		}
	}
	if opts.Output == outputText {
		output.NewContinuation(stdout).Summary(output.StatusOK, "host trust", fmt.Sprintf("%d host(s) checked", len(report.Hosts)))
		printNextStatusHint(stdout)
	}
	return report, nil
}

func hostTrustNeedsScan(state v1alpha1.State, selected map[string]bool) bool {
	for _, machine := range state.Machines {
		if len(selected) > 0 && !selected[machine.Metadata.Name] {
			continue
		}
		if !machineNeedsManagedHostTrust(machine, controllerLocalityPolicy) {
			continue
		}
		return true
	}
	return false
}

type hostTrustPlan struct {
	reports       []hostTrustHostReport
	records       []sshtrust.HostRecord
	pendingWrites int
}

func buildHostTrustPlan(ctx context.Context, state v1alpha1.State, store sshtrust.Store, selected, replace map[string]bool, scan func(context.Context, string, time.Duration) ([]sshtrust.ScannedKey, error)) (hostTrustPlan, error) {
	var plan hostTrustPlan
	machines := append([]v1alpha1.Machine(nil), state.Machines...)
	sort.Slice(machines, func(i, j int) bool { return machines[i].Metadata.Name < machines[j].Metadata.Name })
	seen := map[string]bool{}
	for _, machine := range machines {
		if len(selected) > 0 && !selected[machine.Metadata.Name] {
			continue
		}
		seen[machine.Metadata.Name] = true
		report, record, write, err := evaluateHostTrust(ctx, machine, store, replace[machine.Metadata.Name], scan)
		if err != nil {
			return plan, err
		}
		plan.reports = append(plan.reports, report)
		if write {
			plan.records = append(plan.records, record)
			plan.pendingWrites++
		}
	}
	for name := range selected {
		if !seen[name] {
			return plan, fmt.Errorf("host/%s does not exist", name)
		}
	}
	for name := range replace {
		if len(selected) > 0 && !selected[name] {
			return plan, fmt.Errorf("--replace host %q is outside --hosts selection", name)
		}
		if !seen[name] {
			return plan, fmt.Errorf("--replace host %q does not use managed SSH trust", name)
		}
	}
	return plan, nil
}

func evaluateHostTrust(ctx context.Context, machine v1alpha1.Machine, store sshtrust.Store, replace bool, scan func(context.Context, string, time.Duration) ([]sshtrust.ScannedKey, error)) (hostTrustHostReport, sshtrust.HostRecord, bool, error) {
	report := hostTrustHostReport{Name: machine.Metadata.Name}
	if machine.Spec.Access.SSH == nil {
		report.Action = "skip"
		report.Reason = "Machine has no spec.access.ssh"
		return report, sshtrust.HostRecord{}, false, nil
	}
	report.Address = v1alpha1.MachineSSHAddress(machine)
	if locality.IsControllerLocalMachine(machine, controllerLocalityPolicy) {
		report.Action = "skip"
		report.Reason = "controller-local Machine"
		return report, sshtrust.HostRecord{}, false, nil
	}
	if machine.Spec.Access.SSH.KnownHostsRef.Name != "" {
		report.Action = "skip"
		report.Reason = "uses explicit knownHostsRef"
		return report, sshtrust.HostRecord{}, false, nil
	}
	if !v1alpha1.MachineOSProvided(machine) {
		report.Action = "skip"
		report.Reason = "Machine OS is not provided"
		return report, sshtrust.HostRecord{}, false, nil
	}
	keys, err := scan(ctx, report.Address, defaultHostTrustScanTimeout)
	if err != nil {
		return report, sshtrust.HostRecord{}, false, fmt.Errorf("scan Machine/%s at %s: %w", machine.Metadata.Name, report.Address, err)
	}
	key, ok := sshtrust.SelectPreferred(keys)
	if !ok {
		return report, sshtrust.HostRecord{}, false, fmt.Errorf("scan Machine/%s at %s: no SSH host keys returned", machine.Metadata.Name, report.Address)
	}
	record := sshtrust.HostRecord{
		Name:              machine.Metadata.Name,
		Address:           report.Address,
		KeyType:           key.KeyType,
		PublicKey:         key.PublicKey,
		FingerprintSHA256: key.FingerprintSHA256,
		KnownHostsLine:    key.KnownHostsLine,
	}
	report.KeyType = record.KeyType
	report.FingerprintSHA256 = record.FingerprintSHA256
	previous, exists := store.Find(machine.Metadata.Name)
	if !exists {
		report.Action = "add"
		return report, record, true, nil
	}
	report.PreviousAddress = previous.Address
	report.PreviousFingerprintSHA256 = previous.FingerprintSHA256
	if previous.Address == record.Address && previous.PublicKey == record.PublicKey && previous.KeyType == record.KeyType {
		report.Action = "reuse"
		return report, record, false, nil
	}
	if !replace {
		return report, sshtrust.HostRecord{}, false, fmt.Errorf("machine/%s SSH trust changed; old %s %s, new %s %s; rerun with --replace %s after verifying the new fingerprint", machine.Metadata.Name, previous.Address, previous.FingerprintSHA256, record.Address, record.FingerprintSHA256, machine.Metadata.Name)
	}
	report.Action = "replace"
	return report, record, true, nil
}

func scanSSHHostKeys(ctx context.Context, address string, timeout time.Duration) ([]sshtrust.ScannedKey, error) {
	if strings.TrimSpace(address) == "" {
		return nil, errors.New("address is required")
	}
	seconds := int(timeout.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout+time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "ssh-keyscan", "-T", strconv.Itoa(seconds), "-t", "ed25519,ecdsa,rsa", address)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return sshtrust.ParseScanOutput(address, out)
}

func printHostTrustPlan(stdout io.Writer, report hostTrustReport) {
	p := output.New(stdout)
	p.Command("host trust")
	p.Section("Trust store")
	p.Fields([]output.Field{
		{Key: "context", Value: report.Context},
		{Key: "known-hosts", Value: report.KnownHostsPath},
		{Key: "store", Value: report.StorePath},
	})
	p.Section("Hosts")
	if len(report.Hosts) == 0 {
		p.Status(output.StatusSkip, "hosts", "no managed SSH trust targets")
		return
	}
	for _, host := range report.Hosts {
		status := output.StatusOK
		switch host.Action {
		case "add", "replace":
			status = output.StatusWarn
		case "skip":
			status = output.StatusSkip
		}
		p.Status(status, host.Name, hostTrustDetail(host))
	}
}

func hostTrustDetail(host hostTrustHostReport) string {
	if host.Action == "skip" {
		return host.Reason
	}
	detail := host.Action + " " + host.Address + " " + host.KeyType + " " + host.FingerprintSHA256
	if host.PreviousFingerprintSHA256 != "" && (host.PreviousAddress != host.Address || host.PreviousFingerprintSHA256 != host.FingerprintSHA256) {
		detail += " (was " + host.PreviousAddress + " " + host.PreviousFingerprintSHA256 + ")"
	}
	return detail
}

func parseNameSet(value string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, raw := range strings.Split(value, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if !isNameToken(name) {
			return nil, fmt.Errorf("invalid Machine name %q", name)
		}
		out[name] = true
	}
	return out, nil
}

func isNameToken(value string) bool {
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return value != ""
}
