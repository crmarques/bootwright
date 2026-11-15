package cli

import (
	"encoding/json"
	"io"
	"slices"

	containeraccess "github.com/crmarques/bootwright/internal/containercluster/access"
)

type clusterHandoffPresentation struct {
	Context           string                    `json:"context"`
	Cluster           string                    `json:"cluster"`
	Client            handoffClientPresentation `json:"client"`
	Arguments         []string                  `json:"arguments"`
	KubeconfigCommand []string                  `json:"kubeconfigCommand"`
	Instructions      string                    `json:"instructions"`
}

type handoffClientPresentation struct {
	Executable   string `json:"executable"`
	Version      string `json:"version"`
	Distribution string `json:"distribution"`
}

func (clusterHandoffPresentation) documentedResult() {}

func displayHandoff(result *containeraccess.Handoff) clusterHandoffPresentation {
	return clusterHandoffPresentation{
		Context: result.Context, Cluster: result.Cluster,
		Client:    handoffClientPresentation{Executable: result.Client.Executable, Version: result.Client.Version, Distribution: result.Client.Distribution},
		Arguments: slices.Clone(result.Arguments), KubeconfigCommand: slices.Clone(result.KubeconfigCommand),
		Instructions: "Export the kubeconfig into a private mode-0600 file. Run the absolute client executable with --kubeconfig pointing to that file, --cache-dir pointing to a private temporary directory, and the arguments as separate values. Use a clean environment and private working directory; do not load ambient configuration or plugins. Remove the private files after use. No client has been launched.",
	}
}

func writeHandoff(out io.Writer, result *containeraccess.Handoff) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(displayHandoff(result))
}
