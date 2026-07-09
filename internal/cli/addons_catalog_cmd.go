package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/addons/nativecatalog"
	"github.com/crmarques/bootwright/internal/cli/output"
)

type addonsListReport struct {
	AddOns []addonsListEntry `json:"addOns"`
}

type addonsListEntry struct {
	Name           string   `json:"name"`
	Description    string   `json:"description,omitempty"`
	DefaultVersion string   `json:"defaultVersion"`
	Versions       []string `json:"versions"`
	Registered     string   `json:"registered,omitempty"`
	Modified       bool     `json:"modified,omitempty"`
}

func newAddonsCatalogCmd(stdin io.Reader, stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-ons",
		Short: "Manage registered native add-ons",
		Long: `Manage the machine-local store of native add-ons vended from the built-in
catalog. A registered add-on resolves any ClusterAddonBinding addonRef that no
authored ClusterAddon matches; context init snapshots it into the context, so
the context keeps working even if the add-on is later re-registered or
deleted. Bindings are authored as usual — registering an add-on makes it
available, binding it to a cluster is the separate, explicit step.`,
	}
	cmd.AddCommand(
		newAddonsListCmd(stdout),
		newAddonsAddCmd(stdin, stdout),
		newAddonsDeleteCmd(stdin, stdout),
	)
	requireSubcommand(cmd)
	return cmd
}

func newAddonsListCmd(stdout io.Writer) *cobra.Command {
	var outputFormat string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List catalog add-ons and their registered versions",
		Args:  cobra.NoArgs,
	}
	addOutputFlag(cmd, &outputFormat)
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if err := validateOutputFormat(outputFormat); err != nil {
			return failErr(2, err)
		}
		entries, err := nativecatalog.Entries()
		if err != nil {
			return failErr(1, err)
		}
		installed, err := nativecatalog.InstalledAddons()
		if err != nil {
			return failErr(1, err)
		}
		registered := map[string]nativecatalog.Installed{}
		for _, item := range installed {
			registered[item.Marker.Name] = item
		}
		report := addonsListReport{}
		for _, entry := range entries {
			row := addonsListEntry{
				Name:           entry.Name,
				Description:    entry.Description,
				DefaultVersion: entry.DefaultVersion,
			}
			for _, version := range entry.Versions {
				row.Versions = append(row.Versions, version.Version)
			}
			if item, ok := registered[entry.Name]; ok {
				row.Registered = item.Marker.Version
				row.Modified = item.Modified
			}
			report.AddOns = append(report.AddOns, row)
		}
		if outputFormat == outputJSON {
			return output.JSON(stdout, report)
		}
		p := output.New(stdout)
		p.Command("add-ons list")
		checks := make([]output.Check, 0, len(report.AddOns))
		for _, row := range report.AddOns {
			status := output.StatusSkip
			evidence := fmt.Sprintf("versions %v (default %s), not registered", row.Versions, row.DefaultVersion)
			if row.Registered != "" {
				status = output.StatusOK
				evidence = fmt.Sprintf("registered %s of %v", row.Registered, row.Versions)
				if row.Modified {
					evidence += " (locally modified)"
				}
			}
			checks = append(checks, output.Check{
				Group:    "Native add-on catalog",
				Name:     row.Name,
				Status:   status,
				Evidence: evidence,
			})
		}
		p.Checks(checks)
		return nil
	}
	return cmd
}

func newAddonsAddCmd(stdin io.Reader, stdout io.Writer) *cobra.Command {
	var (
		name    string
		version string
		yes     bool
	)
	cmd := &cobra.Command{
		Use:   "add --name <name>[:<version>]",
		Short: "Register a catalog add-on in the machine-local store",
		Args:  cobra.NoArgs,
		Example: `  bootwright add-ons add --name openshift-data-foundation
  bootwright add-ons add --name openshift-data-foundation:4.21
  bootwright add-ons add --name fusion-data-foundation --version 4.21 --yes`,
	}
	cmd.Flags().StringVar(&name, "name", "", "catalog add-on name, optionally <name>:<version> (required)")
	cmd.Flags().StringVar(&version, "version", "", "catalog version (default: the entry's default version)")
	addYesFlag(cmd, &yes, "replace")
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if name == "" {
			return failf(2, "--name is required")
		}
		bare, inlineVersion := nativecatalog.ParseNameVersion(name)
		if inlineVersion != "" {
			if version != "" {
				return failf(2, "--version conflicts with the <name>:<version> form of --name")
			}
			version = inlineVersion
		}
		release, err := nativecatalog.Resolve(bare, version)
		if err != nil {
			return failErr(1, err)
		}
		if marker, found, err := nativecatalog.ReadMarker(nativecatalog.InstalledDir(bare)); err != nil {
			return failErr(1, err)
		} else if found && !yes && !confirm(stdin, stdout, fmt.Sprintf("Replace registered add-on %s %s with %s? [y/N] (default: no): ", marker.Name, marker.Version, release.Version)) {
			return failErr(1, errors.New("add-ons add aborted"))
		}
		dir, err := nativecatalog.Install(release)
		if err != nil {
			return failErr(1, err)
		}
		p := output.New(stdout)
		p.Command("add-ons add")
		p.Section("Registered add-on")
		p.Status(output.StatusOK, release.Entry.Name, "version "+release.Version)
		p.Status(output.StatusOK, "store dir", dir)
		p.Summary(output.StatusOK, release.Entry.Name,
			"registered; bind it with a ClusterAddonBinding addonRef, then bootwright context init/update snapshots it into the context")
		return nil
	}
	return cmd
}

func newAddonsDeleteCmd(stdin io.Reader, stdout io.Writer) *cobra.Command {
	var name string
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete --name <name>[:<version>]",
		Short: "Delete a registered add-on from the machine-local store",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().StringVar(&name, "name", "", "registered add-on name, optionally <name>:<version> (required)")
	addYesFlag(cmd, &yes, "delete")
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if name == "" {
			return failf(2, "--name is required")
		}
		// Accept the same <name>:<version> shorthand add teaches; the store holds
		// one version per name, so the version is an assertion, not a selector.
		bare, inlineVersion := nativecatalog.ParseNameVersion(name)
		output.New(stdout).Command("add-ons delete")
		dir := nativecatalog.InstalledDir(bare)
		marker, found, err := nativecatalog.ReadMarker(dir)
		if err != nil {
			return failErr(1, err)
		}
		if !found {
			if _, statErr := os.Stat(dir); errors.Is(statErr, os.ErrNotExist) {
				return failf(1, "add-on %q is not registered", bare)
			}
			return failf(1, "%s was not registered by bootwright add-ons add; remove it manually", dir)
		}
		if inlineVersion != "" && marker.Version != inlineVersion {
			return failf(1, "add-on %q is registered at version %s, not %s", bare, marker.Version, inlineVersion)
		}
		if !yes && !confirm(stdin, stdout, fmt.Sprintf("Delete registered add-on %s %s from %s? Existing contexts keep their snapshotted copy. [y/N] (default: no): ", marker.Name, marker.Version, dir)) {
			return failErr(1, errors.New("add-ons delete aborted"))
		}
		if err := nativecatalog.Remove(bare); err != nil {
			return failErr(1, err)
		}
		output.NewContinuation(stdout).Summary(output.StatusOK, bare, "removed "+dir)
		return nil
	}
	return cmd
}
