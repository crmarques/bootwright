package cli

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestResourceLayoutsPreserveFactsAcrossWidths(t *testing.T) {
	machines := machineList()
	machines.PowerRead = true
	machines.Machines[0].Power = "on"
	machines.Machines[1].Power = "unknown"
	for _, width := range []int{24, 48, 96, 220} {
		for _, color := range []string{"never", "always"} {
			for _, ascii := range []bool{false, true} {
				for _, test := range []struct {
					name  string
					write func(io.Writer) error
					facts []string
				}{
					{"machine-list", func(out io.Writer) error { return writeMachineList(out, "machine list", machines, false, false) }, []string{"guest", "guest.lab.example.test", "198.51.100.20,192.0.2.20", "installed", "lab", "applied", "on", "node", "provided", "ocp", "not-applied", "unknown"}},
					{"machine-info", func(out io.Writer) error { return writeMachineInfo(out, "machine info", machines, false) }, []string{"guest", "guest.lab.example.test", "198.51.100.20, 192.0.2.20", "installed", "lab", "applied", "node", "provided", "ocp", "not-applied", "unknown", "Placement", "Access", "Evidence"}},
					{"secret-list", func(out io.Writer) error { return writeSecretList(out, "secret list", infoSecrets(), false) }, []string{"alpha", "alpha-two", "token", "contextStore", "value", "current", "v2", "sshKeyPair", "generated", "private-key", "public-key", "orphaned"}},
					{"secret-info", func(out io.Writer) error { return writeSecretInfo(out, "secret info", infoSecrets(), false) }, []string{"alpha", "alpha-two", "token", "contextStore", "value", "current", "v2", "sshKeyPair", "generated", "private-key", "public-key", "orphaned", "version-opaque-id", "Identity", "Custody"}},
					{"cluster-list", func(out io.Writer) error { return writeClusterList(out, "cluster list", clusterListResult(), false) }, []string{"archive", "edge", "okd-lab", "storage", "ContainerCluster", "StorageCluster"}},
					{"cluster-info", func(out io.Writer) error {
						return writeClusterInfo(out, io.Discard, "cluster info", storageOnly(readyCeph()), false)
					}, []string{"storage", "StorageCluster", "Topology", "Access", "Evidence", "Custody", "9.9.1.2", "ready", "FSID", "Digest", "Certificate SHA-256", "hidden; rerun with --secrets --name storage"}},
				} {
					t.Run(fmt.Sprintf("%s/%d/%s/ascii=%t", test.name, width, color, ascii), func(t *testing.T) {
						var output bytes.Buffer
						writer := WithPresentation(&output, PresentationOptions{Width: width, Color: color, ASCII: ascii, Detail: "full"})
						if err := test.write(writer); err != nil {
							t.Fatal(err)
						}
						plain := ansi.Strip(output.String())
						compact := strings.Join(strings.Fields(plain), "")
						for _, fact := range test.facts {
							if !strings.Contains(compact, strings.Join(strings.Fields(fact), "")) {
								t.Errorf("missing fact %q in %q", fact, plain)
							}
						}
						for _, line := range strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n") {
							if got := ansi.StringWidth(line); got > width-1 {
								t.Errorf("line width=%d beyond %d: %q", got, width-1, line)
							}
						}
						if color == "never" && strings.Contains(output.String(), "\x1b") {
							t.Fatal("color=never emitted ANSI")
						}
					})
				}
			}
		}
	}
}

func TestResourceInfoCompactDetailOmitsOnlyVerboseFacts(t *testing.T) {
	for _, test := range []struct {
		name     string
		write    func(io.Writer) error
		common   []string
		fullOnly []string
	}{
		{"machine", func(out io.Writer) error { return writeMachineInfo(out, "machine info", machineList(), false) }, []string{"Placement", "Access", "Evidence", "guest.lab.example.test", "not read"}, []string{"198.51.100.20", "Provider"}},
		{"secret", func(out io.Writer) error { return writeSecretInfo(out, "secret info", infoSecrets(), false) }, []string{"Identity", "Custody", "contextStore", "v2", "Bound versions"}, []string{"version-opaque-id", "Version ID"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var compact, full bytes.Buffer
			for _, view := range []struct {
				out    *bytes.Buffer
				detail string
			}{{&compact, "compact"}, {&full, "full"}} {
				if err := test.write(WithPresentation(view.out, PresentationOptions{Width: 220, Detail: view.detail})); err != nil {
					t.Fatal(err)
				}
			}
			for _, fact := range test.common {
				if !strings.Contains(compact.String(), fact) || !strings.Contains(full.String(), fact) {
					t.Errorf("common fact missing %q: compact=%q full=%q", fact, compact.String(), full.String())
				}
			}
			for _, fact := range test.fullOnly {
				if strings.Contains(compact.String(), fact) || !strings.Contains(full.String(), fact) {
					t.Errorf("detail policy for %q: compact=%q full=%q", fact, compact.String(), full.String())
				}
			}
		})
	}
}
