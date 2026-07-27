package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
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
		newMediaAddCmd(stdin, stdout),
		newMediaListCmd(stdout),
		newMediaDeleteCmd(stdin, stdout),
	)
	requireSubcommand(cmd)
	return cmd
}

func newMediaAddCmd(stdin io.Reader, stdout io.Writer) *cobra.Command {
	var (
		name     string
		fromFile string
		fromURL  string
		sum      string
		yes      bool
	)
	cmd := &cobra.Command{
		Use:   "add --name <filename.iso>",
		Short: "Add an ISO to the managed media store",
		Args:  cobra.NoArgs,
		Example: `  bootwright media add --name rhel-9-x86_64-dvd.iso --from-file /path/to/rhel.iso
  bootwright media add --name rhel-9-x86_64-dvd.iso --from-url http://mirror.example.test/rhel.iso --sha256 <hex>`,
	}
	cmd.Flags().StringVar(&name, "name", "", "media store filename, e.g. rhel-9-x86_64-dvd.iso (required)")
	cmd.Flags().StringVar(&fromFile, "from-file", "", "copy ISO bytes from a local file")
	cmd.Flags().StringVar(&fromURL, "from-url", "", "download ISO bytes from an HTTP(S) URL")
	cmd.Flags().StringVar(&sum, "sha256", "", "expected ISO SHA-256 checksum")
	addYesFlag(cmd, &yes, "replace")
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if name == "" {
			return failf(2, "--name is required")
		}
		if (fromFile == "") == (fromURL == "") {
			return failf(2, "exactly one of --from-file or --from-url is required")
		}
		exists, err := mediaEntryExists(name)
		if err != nil {
			return failErr(1, err)
		}
		if exists && !yes && !confirm(stdin, stdout, fmt.Sprintf("Replace media %s in %s? [y/N] (default: no): ", name, media.StoreDir())) {
			return failErr(1, errors.New("media add aborted"))
		}
		var (
			entry media.Entry
		)
		if fromFile != "" {
			entry, err = media.AddFile(name, fromFile, sum, exists)
		} else {
			entry, err = media.AddURL(name, fromURL, sum, exists)
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

func mediaEntryExists(key string) (bool, error) {
	path, err := media.Path(key)
	if err != nil {
		return false, err
	}
	_, err = os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat media target %s: %w", path, err)
	}
	return true, nil
}

func newMediaListCmd(stdout io.Writer) *cobra.Command {
	var outputFormat string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List managed ISO media",
		Args:  cobra.NoArgs,
	}
	addOutputFlag(cmd, &outputFormat)
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

func newMediaDeleteCmd(stdin io.Reader, stdout io.Writer) *cobra.Command {
	var name string
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete --name <filename.iso>",
		Short: "Delete an ISO from the managed media store",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().StringVar(&name, "name", "", "media store filename, e.g. rhel-9-x86_64-dvd.iso (required)")
	addYesFlag(cmd, &yes, "delete")
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		if name == "" {
			return failf(2, "--name is required")
		}
		output.New(stdout).Command("media delete")
		key := name
		if !yes && !confirm(stdin, stdout, fmt.Sprintf("Delete media %s from %s? [y/N] (default: no): ", key, media.StoreDir())) {
			return failErr(1, errors.New("media delete aborted"))
		}
		entry, err := media.Delete(key)
		if err != nil {
			if strings.Contains(err.Error(), "no such file") {
				return failf(1, "media %q not found in %s", key, media.StoreDir())
			}
			return failErr(1, err)
		}
		output.NewContinuation(stdout).Summary(output.StatusOK, entry.Name, "deleted "+entry.Path)
		return nil
	}
	return cmd
}
