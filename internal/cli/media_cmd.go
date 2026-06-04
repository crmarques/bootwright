package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/infra/media"
)

type mediaListReport struct {
	Media []media.Entry `json:"media"`
}

func newMediaCmd(stdin io.Reader, stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "media",
		Short: "Manage root-local ISO media",
		Long:  "Manage non-secret ISO media under /var/lib/bootwright/media.",
	}
	cmd.AddCommand(
		newMediaAddCmd(stdout),
		newMediaListCmd(stdout),
		newMediaRemoveCmd(stdin, stdout),
	)
	requireSubcommand(cmd)
	return cmd
}

func newMediaAddCmd(stdout io.Writer) *cobra.Command {
	var (
		fromFile string
		fromURL  string
		sum      string
		force    bool
	)
	cmd := &cobra.Command{
		Use:   "add <filename.iso>",
		Short: "Add an ISO to the managed media store",
		Args:  cobra.ExactArgs(1),
		Example: `  bootwright media add rhel-9-x86_64-dvd.iso --from-file /path/to/rhel.iso
  bootwright media add rhel-9-x86_64-dvd.iso --from-url http://mirror.example.test/rhel.iso --sha256 <hex>`,
	}
	cmd.Flags().StringVar(&fromFile, "from-file", "", "copy ISO bytes from a local file")
	cmd.Flags().StringVar(&fromURL, "from-url", "", "download ISO bytes from an HTTP(S) URL")
	cmd.Flags().StringVar(&sum, "sha256", "", "expected ISO SHA-256 checksum")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing media entry")
	cmd.RunE = func(_ *cobra.Command, args []string) error {
		if (fromFile == "") == (fromURL == "") {
			return failf(2, "exactly one of --from-file or --from-url is required")
		}
		var (
			entry media.Entry
			err   error
		)
		if fromFile != "" {
			entry, err = media.AddFile(args[0], fromFile, sum, force)
		} else {
			entry, err = media.AddURL(args[0], fromURL, sum, force)
		}
		if err != nil {
			return failErr(1, err)
		}
		p := output.New(stdout)
		p.Command("media add")
		p.Summary(output.StatusOK, entry.Name, fmt.Sprintf("stored at %s", entry.Path))
		return nil
	}
	return cmd
}

func newMediaListCmd(stdout io.Writer) *cobra.Command {
	outputFormat := outputText
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List managed ISO media",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().StringVar(&outputFormat, "output", outputFormat, "output format: text|json")
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if err := validateOutputFormat(outputFormat); err != nil {
			return failErr(2, err)
		}
		entries, err := media.List()
		if err != nil {
			return failErr(1, err)
		}
		if outputFormat == outputJSON {
			return output.JSON(stdout, mediaListReport{Media: entries})
		}
		p := output.New(stdout)
		p.Command("media list")
		if len(entries) == 0 {
			p.Summary(output.StatusSkip, "media", "none registered")
			return nil
		}
		checks := make([]output.Check, 0, len(entries))
		for _, entry := range entries {
			checks = append(checks, output.Check{
				Group:    "Managed ISO media",
				Name:     entry.Name,
				Status:   output.StatusOK,
				Evidence: fmt.Sprintf("%s %d bytes sha256:%s", entry.Reference, entry.Size, entry.SHA256),
			})
		}
		p.Checks(checks)
		return nil
	}
	return cmd
}

func newMediaRemoveCmd(stdin io.Reader, stdout io.Writer) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "remove <filename.iso>",
		Aliases: []string{"rm"},
		Short:   "Remove an ISO from the managed media store",
		Args:    cobra.ExactArgs(1),
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the remove confirmation prompt")
	cmd.RunE = func(_ *cobra.Command, args []string) error {
		key := args[0]
		if !yes && !confirm(stdin, stdout, fmt.Sprintf("Remove media %s from %s? [y/N] (default: no): ", key, media.StoreDir())) {
			return failErr(1, errors.New("media remove aborted"))
		}
		entry, err := media.Remove(key)
		if err != nil {
			if strings.Contains(err.Error(), "no such file") {
				return failf(1, "media %q not found in %s", key, media.StoreDir())
			}
			return failErr(1, err)
		}
		output.New(stdout).Summary(output.StatusOK, entry.Name, "removed "+entry.Path)
		return nil
	}
	return cmd
}
