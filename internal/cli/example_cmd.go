package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/state/desired"
	"github.com/crmarques/bootwright/internal/state/scaffold"
)

func newExampleCmd(stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "example <command>",
		Short: "Create desired-state example input files",
	}
	cmd.AddCommand(newExampleInitCmd(stdout))
	requireSubcommand(cmd)
	return cmd
}

func newExampleInitCmd(stdout io.Writer) *cobra.Command {
	name := ""
	provider := string(scaffold.ProviderEmulatedBareMetal)
	outputDir := ""
	yes := false
	cmd := &cobra.Command{
		Use:   "init --name <cluster-name>",
		Short: "Write a safe desired-state example directory",
		Args:  cobra.NoArgs,
		Example: `  bootwright example init --name my-sno-lab --output-dir ./my-sno-lab
  bootwright example init --name my-baremetal-lab --provider bare-metal --output-dir ./my-baremetal-lab`,
	}
	cmd.Flags().StringVar(&name, "name", "", "cluster name to scaffold (required)")
	cmd.Flags().StringVar(&provider, "provider", provider, "example provider ("+strings.Join(scaffold.KnownProviders(), "|")+")")
	registerFlagCompletion(cmd, "provider", scaffold.KnownProviders())
	cmd.Flags().StringVar(&outputDir, "output-dir", outputDir, "directory to write the example into (default: the --name value)")
	cmd.Flags().BoolVar(&yes, "yes", false, "overwrite files in a non-empty output directory")
	cmd.RunE = func(_ *cobra.Command, _ []string) error {
		clusterName := name
		if !desiredstate.IsDNSLabel(clusterName) {
			return failf(2, "--name must be a lowercase DNS label")
		}
		if outputDir == "" {
			outputDir = clusterName
		}
		files, err := scaffold.Workspace(clusterName, scaffold.Provider(provider))
		if err != nil {
			return failErr(2, err)
		}
		written, err := writeExampleFiles(outputDir, files, yes)
		if err != nil {
			return failErr(1, err)
		}
		support := scaffold.ApplySupport(scaffold.Provider(provider))
		p := output.New(stdout)
		p.Command("example init")
		p.Section("Example")
		p.Fields([]output.Field{
			{Key: "cluster", Value: clusterName},
			{Key: "provider", Value: provider},
			{Key: "output-dir", Value: filepath.Clean(outputDir)},
			{Key: "apply support", Value: fmt.Sprintf("%s - %s", support.Status, support.Summary)},
		})
		p.Section("Written inputs")
		p.Artifacts([]output.ArtifactGroup{{Paths: written}})
		p.Summary(output.StatusOK, "example init", "import with bootwright context init --name <ctx> -f "+filepath.Clean(outputDir))
		return nil
	}
	return cmd
}

func writeExampleFiles(dir string, files []scaffold.File, overwrite bool) ([]string, error) {
	cleanDir := filepath.Clean(dir)
	if err := prepareExampleOutputDir(cleanDir, overwrite); err != nil {
		return nil, err
	}
	written := make([]string, 0, len(files))
	for _, file := range files {
		name := filepath.Clean(file.Name)
		if name == "." || filepath.IsAbs(name) || strings.HasPrefix(name, ".."+string(filepath.Separator)) || name == ".." {
			return nil, fmt.Errorf("refusing unsafe scaffold filename %q", file.Name)
		}
		path := filepath.Join(cleanDir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(file.Body), 0o644); err != nil {
			return nil, err
		}
		written = append(written, path)
	}
	return written, nil
}

func prepareExampleOutputDir(dir string, overwrite bool) error {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return os.MkdirAll(dir, 0o755)
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("output path is not a directory: %s", dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) > 0 && !overwrite {
		return fmt.Errorf("output directory %s is not empty; rerun with --yes to overwrite scaffolded files", dir)
	}
	return nil
}
