package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
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

func TestTheTrustPlanNamesEveryPendingFingerprint(t *testing.T) {
	var out bytes.Buffer
	if err := NewTrustPlanPresenter(&out).PresentTrustPlan(context.Background(), *trustReport()); err != nil {
		t.Fatal(err)
	}
	want := "Host-key trust plan for context lab: 3 machine(s) checked, 2 pending\n" +
		"\n" +
		"MACHINE    ADDRESS            ACTION   KEY          FINGERPRINT\n" +
		"node-a     192.0.2.10         add      ssh-ed25519  SHA256:aaa\n" +
		"node-b     [192.0.2.11]:2222  replace  ssh-ed25519  SHA256:bbb (was SHA256:ccc)\n" +
		"installed  -                  skip     -            host key comes from its installation evidence\n"
	if out.String() != want {
		t.Fatalf("plan = %q, want %q", out.String(), want)
	}
}

// The operator saw the table before confirming, so the result after recording
// repeats only what was recorded.
func TestAPresentedTrustResultRepeatsOnlyItsHeadline(t *testing.T) {
	var out bytes.Buffer
	report := trustReport()
	report.Presented = true
	if err := writeTrustReport(&out, "machine trust", report, false); err != nil {
		t.Fatal(err)
	}
	if out.String() != "[OK] 3 machine(s) checked, 2 recorded\n" {
		t.Fatalf("result = %q", out.String())
	}
}

type failingWriter struct{ short bool }

func (w failingWriter) Write(data []byte) (int, error) {
	if w.short {
		return len(data) / 2, nil
	}
	return 0, errors.New("closed")
}

// A plan that did not reach the operator must not be followed by its prompt.
func TestTheTrustPlanRefusesWhenItCannotBeWritten(t *testing.T) {
	for name, out := range map[string]io.Writer{
		"a failed write": failingWriter{}, "a short write": failingWriter{short: true}, "no output": nil,
	} {
		t.Run(name, func(t *testing.T) {
			if err := NewTrustPlanPresenter(out).PresentTrustPlan(context.Background(), *trustReport()); err == nil {
				t.Fatal("an unwritten plan was reported as presented")
			}
		})
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	var out bytes.Buffer
	if err := NewTrustPlanPresenter(&out).PresentTrustPlan(canceled, *trustReport()); !errors.Is(err, context.Canceled) || out.Len() != 0 {
		t.Fatalf("canceled plan = %v, wrote %q", err, out.String())
	}
}

func TestAReplacementShowsTheEndpointItSupersedes(t *testing.T) {
	for _, test := range []struct {
		name string
		host enrollment.HostReport
		want string
	}{
		{"a changed key", enrollment.HostReport{Fingerprint: "SHA256:bbb", PreviousFingerprint: "SHA256:ccc"}, "SHA256:bbb (was SHA256:ccc)"},
		{
			"a moved endpoint", enrollment.HostReport{Fingerprint: "SHA256:aaa", PreviousFingerprint: "SHA256:aaa", PreviousAddress: "192.0.2.99", PreviousPort: 22},
			"SHA256:aaa (unchanged; was trusted at 192.0.2.99)",
		},
		{
			"both", enrollment.HostReport{Fingerprint: "SHA256:bbb", PreviousFingerprint: "SHA256:ccc", PreviousAddress: "192.0.2.99", PreviousPort: 2200},
			"SHA256:bbb (was SHA256:ccc at [192.0.2.99]:2200)",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			host := test.host
			host.Machine, host.Address, host.Port, host.Action = "node-a", "192.0.2.10", 22, enrollment.ActionReplace
			if got := trustDetail(host); got != test.want {
				t.Fatalf("detail = %q, want %q", got, test.want)
			}
		})
	}
	var out bytes.Buffer
	report := &enrollment.Report{Context: "lab", Pending: 1, Recorded: 1, Hosts: []enrollment.HostReport{{
		Machine: "node-a", Address: "192.0.2.10", Port: 22, Action: enrollment.ActionReplace, KeyType: "ssh-ed25519",
		Fingerprint: "SHA256:aaa", PreviousFingerprint: "SHA256:aaa", PreviousAddress: "192.0.2.99", PreviousPort: 22,
	}}}
	if err := writeTrustReport(&out, "machine trust", report, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"previousAddress":"192.0.2.99","previousPort":22`) {
		t.Fatalf("JSON = %s", out.String())
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
