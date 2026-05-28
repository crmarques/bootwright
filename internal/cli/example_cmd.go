package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/scaffold"
)

func newExampleCmd(stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "example <command>",
		Short: "Create desired-state example input files",
	}
	cmd.AddCommand(newExampleInitCmd(stdout))
	requireSubcommand(cmd)
	showSubcommandFlagsInHelp(cmd)
	return cmd
}

func newExampleInitCmd(stdout io.Writer) *cobra.Command {
	provider := string(scaffold.ProviderEmulatedBareMetal)
	outputDir := ""
	yes := false
	cmd := &cobra.Command{
		Use:   "init <cluster-name>",
		Short: "Write a safe desired-state example directory",
		Args:  cobra.ExactArgs(1),
		Example: `  bootwright example init my-sno-lab --output ./my-sno-lab
  bootwright example init my-baremetal-lab --provider bare-metal --output ./my-baremetal-lab`,
	}
	cmd.Flags().StringVar(&provider, "provider", provider, "example provider: "+strings.Join(scaffold.KnownProviders(), "|"))
	cmd.Flags().StringVar(&outputDir, "output", outputDir, "directory to write (defaults to <cluster-name>)")
	cmd.Flags().BoolVar(&yes, "yes", false, "overwrite scaffolded files in a non-empty output directory")
	cmd.RunE = func(_ *cobra.Command, args []string) error {
		clusterName := args[0]
		if !desiredstate.IsDNSLabel(clusterName) {
			return failf(2, "<cluster-name> must be a lowercase DNS label")
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
			{Key: "output", Value: filepath.Clean(outputDir)},
			{Key: "apply support", Value: fmt.Sprintf("%s - %s", support.Status, support.Summary)},
		})
		p.Section("Written inputs")
		p.Artifacts([]output.ArtifactGroup{{Paths: written}})
		p.Summary(output.StatusOK, "example init", "import with bootwright context init <ctx> -f "+filepath.Clean(outputDir))
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
