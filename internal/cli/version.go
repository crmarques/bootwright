package cli

import (
	"io"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/cli/output"
)

// versionString and gitCommit are stamped at build time via -ldflags.
// Default values keep `go build ./...` and `go test ./...` working
// without the Makefile.
var (
	versionString = "dev"
	gitCommit     = "unknown"
)

func bundleVersionMarker() string {
	return "version=" + versionString + "\ngitCommit=" + gitCommit
}

func newVersionCmd(stdout io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build version, git commit, and Go runtime",
		Args:  cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			p := output.New(stdout)
			p.Command("version")
			p.Section("Build")
			p.Fields([]output.Field{
				{Key: "version", Value: versionString},
				{Key: "git commit", Value: gitCommit},
				{Key: "go", Value: runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH},
			})
		},
	}
}
