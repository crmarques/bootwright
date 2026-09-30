package cli

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/trust/enrollment"
)

func trustReport() *enrollment.Report {
	return &enrollment.Report{
		Context: "lab", Pending: 2, Recorded: 2,
		Hosts: []enrollment.HostReport{
			{Machine: "node-a", Address: "192.0.2.10", Port: 22, Action: enrollment.ActionAdd,
				KeyType: "ssh-ed25519", Fingerprint: "SHA256:aaa"},
			{Machine: "node-b", Address: "192.0.2.11", Port: 2222, Action: enrollment.ActionReplace,
				KeyType: "ssh-ed25519", Fingerprint: "SHA256:bbb", PreviousFingerprint: "SHA256:ccc"},
			{Machine: "installed", Action: enrollment.ActionSkip, Reason: "host key comes from its installation evidence"},
		},
	}
}

func TestTrustReportNamesEveryEndpointActionAndFingerprint(t *testing.T) {
	var out bytes.Buffer
	if err := writeTrustReport(&out, "machine trust", trustReport(), false); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.HasPrefix(text, "[OK] 3 machine(s) checked, 2 recorded") {
		t.Fatalf("headline = %q", text)
	}
	for _, expected := range []string{
		"MACHINE", "192.0.2.10", "[192.0.2.11]:2222", "add", "replace", "skip",
		"SHA256:bbb (was SHA256:ccc)", "host key comes from its installation evidence",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("report missing %q:\n%s", expected, text)
		}
	}
}

// A dry run must never read as though it had written something.
func TestADryRunReportSaysNothingWasRecorded(t *testing.T) {
	var out bytes.Buffer
	report := trustReport()
	report.DryRun, report.Recorded = true, 0
	if err := writeTrustReport(&out, "machine trust", report, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "2 pending; nothing was recorded") {
		t.Fatalf("headline = %q", out.String())
	}
}

func TestAnUnchangedTrustReportSaysSo(t *testing.T) {
	var out bytes.Buffer
	report := &enrollment.Report{Context: "lab", Hosts: []enrollment.HostReport{
		{Machine: "node-a", Address: "192.0.2.10", Port: 22, Action: enrollment.ActionReuse, KeyType: "ssh-ed25519"},
	}}
	if err := writeTrustReport(&out, "machine trust", report, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "trust is unchanged") {
		t.Fatalf("headline = %q", out.String())
	}
	var empty bytes.Buffer
	if err := writeTrustReport(&empty, "machine trust", &enrollment.Report{Context: "lab"}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(empty.String(), "[SKIPPED]") {
		t.Fatalf("empty selection = %q", empty.String())
	}
}

func TestTrustReportJSONCarriesTheWholePlan(t *testing.T) {
	var out bytes.Buffer
	if err := writeTrustReport(&out, "machine trust", trustReport(), true); err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		OK       bool              `json:"ok"`
		ExitCode int               `json:"exitCode"`
		Result   enrollment.Report `json:"result"`
		Logs     []string          `json:"logs"`
		Diags    []map[string]any  `json:"diagnostics"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || envelope.ExitCode != 0 || envelope.Result.Recorded != 2 || len(envelope.Result.Hosts) != 3 {
		t.Fatalf("envelope = %+v", envelope)
	}
	if envelope.Result.Hosts[2].Action != enrollment.ActionSkip {
		t.Fatalf("hosts = %+v", envelope.Result.Hosts)
	}
}

// Display escapes each cell once, the fingerprints of a supersede and the
// reason for a skip included.
func TestTrustReportTextEscapesOnce(t *testing.T) {
	raw := func(label string) string { return label + `\x` }
	shown := func(label string) string { return label + `\\x` }
	report := &enrollment.Report{Context: "lab", Pending: 1, Recorded: 1, Hosts: []enrollment.HostReport{
		{Machine: raw("replaced"), Address: raw("address"), Port: 2222, Action: raw("action"),
			KeyType: raw("key"), Fingerprint: raw("fingerprint"), PreviousFingerprint: raw("previous")},
		{Machine: raw("skipped"), Action: enrollment.ActionSkip, Reason: raw("reason")},
	}}
	var out bytes.Buffer
	if err := writeTrustReport(&out, "machine trust", report, false); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	replaced := []string{
		shown("replaced"), "[" + shown("address") + "]:2222", shown("action"), shown("key"),
		shown("fingerprint"), "(was", shown("previous") + ")",
	}
	skipped := []string{shown("skipped"), "-", "skip", "-", shown("reason")}
	if len(lines) != 5 || !slices.Equal(strings.Fields(lines[3]), replaced) || !slices.Equal(strings.Fields(lines[4]), skipped) {
		t.Fatalf("trust text = %q, want the rows %q and %q", out.String(), replaced, skipped)
	}
}

func TestAnIncompleteTrustReportIsNotPresented(t *testing.T) {
	if validTrustReport(nil) || validTrustReport(&enrollment.Report{}) {
		t.Fatal("an unnamed report was presented")
	}
	if validTrustReport(&enrollment.Report{Context: "lab", Hosts: []enrollment.HostReport{{Machine: "a"}}}) {
		t.Fatal("a host with no action was presented")
	}
}
