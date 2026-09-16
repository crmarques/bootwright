package cli

import (
	"bytes"
	"strings"
	"testing"

	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/machine/power"
)

func machineList() *inventory.ListResult {
	return &inventory.ListResult{Context: "lab", Machines: []inventory.MachineRow{
		{Name: "guest", Address: "192.0.2.20", OS: "installed", Provider: "lab", Clusters: []string{}, State: "owned"},
		{Name: "node", OS: "provided", Clusters: []string{"ocp"}, State: "unmanaged"},
	}}
}

func TestMachineListPresentsEveryRowAndItsAbsentValues(t *testing.T) {
	var out bytes.Buffer
	if err := writeMachineList(&out, "machine list", machineList(), false, false); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "NAME") {
		t.Fatalf("table = %q", out.String())
	}
	if !strings.Contains(lines[1], "guest") || !strings.Contains(lines[1], "192.0.2.20") || !strings.Contains(lines[1], "owned") {
		t.Fatalf("row = %q", lines[1])
	}
	if !strings.Contains(lines[2], "-") || !strings.Contains(lines[2], "ocp") {
		t.Fatalf("a Machine with no contact did not read as absent: %q", lines[2])
	}
}

// A silent invocation is for a consumer, so it prints sorted names and nothing
// else at all.
func TestMachineListSilentPrintsOnlyNames(t *testing.T) {
	var out bytes.Buffer
	if err := writeMachineList(&out, "machine list", machineList(), true, false); err != nil {
		t.Fatal(err)
	}
	if out.String() != "guest\nnode\n" {
		t.Fatalf("silent output = %q", out.String())
	}
	var empty bytes.Buffer
	if err := writeMachineList(&empty, "machine list", &inventory.ListResult{Context: "lab"}, true, false); err != nil {
		t.Fatal(err)
	}
	if empty.Len() != 0 {
		t.Fatalf("an empty selection printed %q", empty.String())
	}
}

func TestMachineListJSONCarriesItsContextAndRows(t *testing.T) {
	var out bytes.Buffer
	if err := writeMachineList(&out, "machine list", machineList(), false, true); err != nil {
		t.Fatal(err)
	}
	want := `{"schemaVersion":"v1alpha1","command":"machine list","ok":true,"exitCode":0,` +
		`"result":{"context":"lab","machines":[` +
		`{"name":"guest","address":"192.0.2.20","os":"installed","provider":"lab","clusters":[],"state":"owned"},` +
		`{"name":"node","address":"","os":"provided","provider":"","clusters":["ocp"],"state":"unmanaged"}` +
		`]},"diagnostics":[],"logs":[]}` + "\n"
	if out.String() != want {
		t.Fatalf("JSON = %q, want %q", out.String(), want)
	}
}

func TestMachineListRefusesToPresentAnIncompleteRow(t *testing.T) {
	if validMachineList(nil) || validMachineList(&inventory.ListResult{}) {
		t.Fatal("an empty result was presented")
	}
	if validMachineList(&inventory.ListResult{Context: "lab", Machines: []inventory.MachineRow{{Name: "guest"}}}) {
		t.Fatal("a row with no state was presented")
	}
}

// One bounded line, quoted so the operator runs exactly the vector this command
// resolved, and nothing else on standard output.
func TestAccessDescriptorIsOneQuotedLine(t *testing.T) {
	var out, errOut bytes.Buffer
	descriptor := &machineaccess.Descriptor{
		Client: "/usr/bin/ssh", Machine: "guest",
		Arguments: []string{"-l", "bootwright", "192.0.2.20", "systemctl status 'sshd'"},
	}
	if err := writeDescriptor(&out, &errOut, "machine exec", descriptor); err != nil {
		t.Fatal(err)
	}
	want := "/usr/bin/ssh -l bootwright 192.0.2.20 'systemctl status '\\''sshd'\\'''\n"
	if out.String() != want {
		t.Fatalf("descriptor = %q, want %q", out.String(), want)
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr = %q", errOut.String())
	}
}

// The advisory names the export the operator performs themselves. It is a
// warning beside the descriptor, never part of the line they run.
func TestAccessAdvisoryStaysOffTheDescriptorLine(t *testing.T) {
	var out, errOut bytes.Buffer
	descriptor := &machineaccess.Descriptor{
		Client: "/usr/bin/ssh", Arguments: []string{"192.0.2.20"},
		Advisory: "this Machine authenticates with the fleet Secret",
	}
	if err := writeDescriptor(&out, &errOut, "machine rsh", descriptor); err != nil {
		t.Fatal(err)
	}
	if out.String() != "/usr/bin/ssh 192.0.2.20\n" {
		t.Fatalf("descriptor = %q", out.String())
	}
	if !strings.HasPrefix(errOut.String(), "[WARN] access.unavailable:") {
		t.Fatalf("advisory = %q", errOut.String())
	}
}

func TestPowerResultReportsTheProvedStateAndWhetherItChanged(t *testing.T) {
	var out bytes.Buffer
	result := &power.Result{Context: "lab", Machine: "guest", Verb: "stop", Power: "off", Previous: "on", Changed: true}
	if err := writeMachinePower(&out, "machine stop", result, false); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "[OK] Machine is off") || !strings.Contains(out.String(), "Previous") {
		t.Fatalf("human output = %q", out.String())
	}
	var settled bytes.Buffer
	unchanged := &power.Result{Context: "lab", Machine: "guest", Verb: "stop", Power: "off", Previous: "off"}
	if err := writeMachinePower(&settled, "machine stop", unchanged, false); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(settled.String(), "[SKIPPED] Machine is off") {
		t.Fatalf("settled output = %q", settled.String())
	}
	var encoded bytes.Buffer
	if err := writeMachinePower(&encoded, "machine stop", result, true); err != nil {
		t.Fatal(err)
	}
	want := `{"schemaVersion":"v1alpha1","command":"machine stop","ok":true,"exitCode":0,` +
		`"result":{"context":"lab","machine":"guest","verb":"stop","power":"off","previous":"on","changed":true},` +
		`"diagnostics":[],"logs":[]}` + "\n"
	if encoded.String() != want {
		t.Fatalf("JSON = %q, want %q", encoded.String(), want)
	}
}

func TestPowerRefusesToPresentAStateItCannotName(t *testing.T) {
	if validMachinePower(nil) || validMachinePower(&power.Result{Machine: "guest", Verb: "stop", Power: "paused"}) {
		t.Fatal("an unreportable power state was presented")
	}
}
