package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/preflight"
	"github.com/crmarques/bootwright/internal/sshtrust"
)

var defaultHostTrustDeps = sshtrust.Deps{
	Scan:     sshtrust.ScanHostKeys,
	LookPath: preflight.DefaultLookPath,
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

func runHostTrust(ctx context.Context, stdin io.Reader, stdout io.Writer, contextName, contextDir string, state v1alpha1.State, opts hostTrustOptions, deps sshtrust.Deps) (sshtrust.Report, error) {
	if deps.Scan == nil {
		deps.Scan = sshtrust.ScanHostKeys
	}
	if deps.LookPath == nil {
		deps.LookPath = preflight.DefaultLookPath
	}
	selected, err := parseNameSet(opts.Hosts)
	if err != nil {
		return sshtrust.Report{}, err
	}
	replace, err := parseNameSet(opts.Replace)
	if err != nil {
		return sshtrust.Report{}, err
	}
	report := sshtrust.Report{
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
	if sshtrust.NeedsScan(state, selected, controllerLocalityPolicy) {
		if _, err := deps.LookPath("ssh-keyscan", nil); err != nil {
			return report, fmt.Errorf("ssh-keyscan is required to record SSH host trust: %w", err)
		}
	}
	plan, err := sshtrust.BuildPlan(ctx, state, store, selected, replace, deps.Scan, controllerLocalityPolicy)
	if err != nil {
		return report, err
	}
	report.Hosts = plan.Reports
	if opts.Output == outputJSON && !opts.DryRun && !opts.Yes && plan.PendingWrites > 0 {
		return report, errors.New("host trust has pending changes; rerun with --yes or use --dry-run with --output json")
	}
	if opts.Output == outputText {
		printHostTrustPlan(stdout, report)
	}
	if opts.DryRun {
		return report, nil
	}
	if plan.PendingWrites > 0 && !opts.Yes && !confirm(stdin, stdout, fmt.Sprintf("Trust %d SSH host key change(s)? [y/N] (default: no): ", plan.PendingWrites)) {
		return report, errors.New("host trust aborted")
	}
	for _, record := range plan.Records {
		store.Upsert(record)
	}
	if len(plan.Records) > 0 || len(sshtrust.ManagedTrustMachines(state, controllerLocalityPolicy)) > 0 {
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

func printHostTrustPlan(stdout io.Writer, report sshtrust.Report) {
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

func hostTrustDetail(host sshtrust.HostReport) string {
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
