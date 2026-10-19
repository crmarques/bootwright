package cli

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/crmarques/bootwright/internal/environment/inspection"
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
					{"machine-info", func(out io.Writer) error { return writeMachineInfo(out, "machine info", machines, false) }, []string{"guest", "guest.lab.example.test", "198.51.100.20, 192.0.2.20", "installed", "lab", "applied", "node", "provided", "ocp", "not-applied", "not read", "Placement", "Access", "Evidence"}},
					{"secret-list", func(out io.Writer) error { return writeSecretList(out, "secret list", infoSecrets(), false) }, []string{"alpha", "alpha-two", "token", "contextStore", "value", "current", "v2", "sshKeyPair", "generated", "private-key", "public-key", "orphaned"}},
					{"secret-info", func(out io.Writer) error { return writeSecretInfo(out, "secret info", infoSecrets(), false) }, []string{"alpha", "alpha-two", "token", "contextStore", "value", "current", "v2", "sshKeyPair", "generated", "private-key", "public-key", "orphaned", "version-opaque-id", "Identity", "Custody"}},
					{"cluster-list", func(out io.Writer) error { return writeClusterList(out, "cluster list", clusterListResult(), false) }, []string{"archive", "edge", "okd-lab", "storage", "ContainerCluster", "StorageCluster"}},
					{"cluster-info", func(out io.Writer) error {
						return writeClusterInfo(out, io.Discard, "cluster info", storageOnly(readyCeph()), false)
					}, []string{"storage", "StorageCluster", "Topology", "Access", "Evidence", "Custody", "9.9.1.2", "ready", "FSID", "Digest", "Certificate SHA-256", "hidden; rerun with --secrets --name storage", "Resources", "StoragePool/rbd-data"}},
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
							if got := ansi.StringWidth(line); got > width-1 && !mayOverflowRow(ansi.Strip(line)) {
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
		{"cluster", func(out io.Writer) error {
			return writeClusterInfo(out, io.Discard, "cluster info", storageOnly(readyCeph()), false)
		}, []string{"Resources", "StoragePool/rbd-data", "done"}, []string{"replicated size 2/min 2, application rbd", "client.rbd-canary read-write on pool rbd-data"}},
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

func mayOverflowRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	if len(strings.Fields(trimmed)) <= 1 || strings.HasPrefix(trimmed, "bootwright ") || strings.HasPrefix(trimmed, "Ceph command ") {
		return true
	}
	label, value, found := strings.Cut(trimmed, "  ")
	return found && label != "" && len(strings.Fields(value)) <= 1
}

func TestClusterInfoCompactDetailKeepsDashboardCertificateFingerprint(t *testing.T) {
	var compact bytes.Buffer
	if err := writeClusterInfo(WithPresentation(&compact, PresentationOptions{Width: 220, Detail: "compact"}), io.Discard, "cluster info", storageOnly(readyCeph()), false); err != nil {
		t.Fatal(err)
	}
	for _, fact := range []string{"Certificate SHA-256", "(self-signed; compare before accepting the browser warning)"} {
		if !strings.Contains(compact.String(), fact) {
			t.Errorf("compact cluster info lost %q: %q", fact, compact.String())
		}
	}
}

func infoColumn(t *testing.T, plain, label string) int {
	t.Helper()
	for _, line := range strings.Split(plain, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if rest, found := strings.CutPrefix(trimmed, label+"  "); found {
			return ansi.StringWidth(line) - ansi.StringWidth(strings.TrimLeft(rest, " "))
		}
	}
	t.Fatalf("no %q row in %q", label, plain)
	return 0
}

func renderFullInfo(t *testing.T, write func(io.Writer) error) string {
	t.Helper()
	var output bytes.Buffer
	if err := write(WithPresentation(&output, PresentationOptions{Width: 220, Color: "never", Detail: "full"})); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func requireGapBeforeSecondRecord(t *testing.T, plain, second string) {
	t.Helper()
	if !strings.Contains(plain, "\n\n  ○ "+second) {
		t.Errorf("no blank line before the second resource heading %q in %q", second, plain)
	}
}

func TestMachineInfoGroupsAlignAndRecordsAreSeparated(t *testing.T) {
	plain := renderFullInfo(t, func(out io.Writer) error { return writeMachineInfo(out, "machine info", machineList(), false) })
	if infoColumn(t, plain, "Lifecycle") != infoColumn(t, plain, "Power") {
		t.Errorf("Lifecycle and Power values start in different columns: %q", plain)
	}
	if infoColumn(t, plain, "Contact") != infoColumn(t, plain, "Addresses") {
		t.Errorf("Contact and Addresses values start in different columns: %q", plain)
	}
	requireGapBeforeSecondRecord(t, plain, "machine | node")
}

func TestSecretInfoCustodyAlignsVersionIDAndRecordsAreSeparated(t *testing.T) {
	plain := renderFullInfo(t, func(out io.Writer) error { return writeSecretInfo(out, "secret info", infoSecrets(), false) })
	if infoColumn(t, plain, "Source") != infoColumn(t, plain, "Version ID") {
		t.Errorf("Source and Version ID values start in different columns: %q", plain)
	}
	requireGapBeforeSecondRecord(t, plain, "secret | alpha-two")
}

func TestClusterInfoGroupsAlignAcrossManagedCephAndRecordsAreSeparated(t *testing.T) {
	result := clusterInfoResult(readyCeph(), inspection.AccessCommand{Status: inspection.StatusReady, Node: "rhel-01"}, readyKubeconfig())
	plain := renderFullInfo(t, func(out io.Writer) error { return writeClusterInfo(out, io.Discard, "cluster info", result, false) })
	storage := plain[strings.Index(plain, "cluster | storage"):]
	typeColumn := infoColumn(t, storage, "Type")
	for _, label := range []string{"Management", "Release", "Admin node"} {
		if got := infoColumn(t, storage, label); got != typeColumn {
			t.Errorf("%s value column %d differs from Type column %d: %q", label, got, typeColumn, storage)
		}
	}
	monitors := infoColumn(t, storage, "Monitors")
	continuation := strings.Index(storage, "rhel-02  rhel-02.lab.test")
	line := storage[strings.LastIndex(storage[:continuation], "\n")+1 : continuation]
	if ansi.StringWidth(line) != monitors {
		t.Errorf("Monitors continuation starts at column %d, first Monitors value at %d: %q", ansi.StringWidth(line), monitors, storage)
	}
	for _, second := range []string{"cluster | okd-lab", "cluster | archive", "cluster | storage"} {
		requireGapBeforeSecondRecord(t, plain, second)
	}
}
