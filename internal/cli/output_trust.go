package cli

import (
	"encoding/json"
	"io"
	"strconv"

	"github.com/crmarques/bootwright/internal/trust/enrollment"
)

func validTrustReport(report *enrollment.Report) bool {
	if report == nil || report.Context == "" {
		return false
	}
	for _, host := range report.Hosts {
		if host.Machine == "" || host.Action == "" {
			return false
		}
	}
	return true
}

func writeTrustReport(out io.Writer, command string, report *enrollment.Report, jsonMode bool) error {
	if jsonMode {
		encoder := json.NewEncoder(out)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(commandEnvelope{
			SchemaVersion: "v1alpha1", Command: escapeDisplayLine(command), OK: true, ExitCode: 0,
			Result: displayTrustReport(report), Diagnostics: []diagnostic{}, Logs: []string{},
		})
	}
	var text display
	if len(report.Hosts) == 0 {
		text.headline("SKIPPED", "No machines use context-managed host-key trust")
		return text.writeTo(out)
	}
	text.headline("OK", trustHeadline(report))
	text.section("")
	rows := make([][]string, 0, len(report.Hosts))
	for _, host := range report.Hosts {
		rows = append(rows, []string{
			escapeDisplayLine(host.Machine), displayValue(trustEndpoint(host)),
			escapeDisplayLine(host.Action), displayValue(host.KeyType),
			displayValue(trustDetail(host)),
		})
	}
	text.table([]string{"MACHINE", "ADDRESS", "ACTION", "KEY", "FINGERPRINT"}, rows)
	return text.writeTo(out)
}

// trustHeadline reports what was recorded rather than what was examined, so a
// dry run can never read as though it had written something.
func trustHeadline(report *enrollment.Report) string {
	checked := strconv.Itoa(len(report.Hosts)) + " machine(s) checked"
	if report.DryRun {
		return checked + ", " + strconv.Itoa(report.Pending) + " pending; nothing was recorded"
	}
	if report.Recorded == 0 {
		return checked + "; trust is unchanged"
	}
	return checked + ", " + strconv.Itoa(report.Recorded) + " recorded"
}

func trustEndpoint(host enrollment.HostReport) string {
	if host.Address == "" {
		return ""
	}
	if host.Port == 0 || host.Port == 22 {
		return host.Address
	}
	return "[" + host.Address + "]:" + strconv.Itoa(host.Port)
}

// trustDetail shows the fingerprint an operator compares, and for a supersede
// the one it replaces, so the change is visible in the result itself.
func trustDetail(host enrollment.HostReport) string {
	if host.Action == enrollment.ActionSkip {
		return host.Reason
	}
	if host.PreviousFingerprint != "" && host.PreviousFingerprint != host.Fingerprint {
		return host.Fingerprint + " (was " + host.PreviousFingerprint + ")"
	}
	return host.Fingerprint
}

// trustResult omits exactly the host fields the enrollment report omits when
// empty, so a host row names only what the enrollment resolved for it.
type trustResult struct {
	Context  string      `json:"context"`
	DryRun   bool        `json:"dryRun"`
	Pending  int         `json:"pending"`
	Recorded int         `json:"recorded"`
	Hosts    []trustHost `json:"hosts"`
}

func (trustResult) documentedResult() {}

type trustHost struct {
	Machine             string `json:"machine"`
	Address             string `json:"address,omitempty"`
	Port                int    `json:"port,omitempty"`
	Action              string `json:"action"`
	KeyType             string `json:"keyType,omitempty"`
	Fingerprint         string `json:"fingerprint,omitempty"`
	PreviousFingerprint string `json:"previousFingerprint,omitempty"`
	Reason              string `json:"reason,omitempty"`
}

func displayTrustReport(report *enrollment.Report) trustResult {
	rows := make([]trustHost, 0, len(report.Hosts))
	for _, host := range report.Hosts {
		rows = append(rows, trustHost{
			Machine: escapeDisplayLine(host.Machine), Address: escapeDisplayLine(host.Address), Port: host.Port,
			Action: escapeDisplayLine(host.Action), KeyType: escapeDisplayLine(host.KeyType),
			Fingerprint:         escapeDisplayLine(host.Fingerprint),
			PreviousFingerprint: escapeDisplayLine(host.PreviousFingerprint),
			Reason:              escapeDisplayLine(host.Reason),
		})
	}
	return trustResult{
		Context: escapeDisplayLine(report.Context), DryRun: report.DryRun,
		Pending: report.Pending, Recorded: report.Recorded, Hosts: rows,
	}
}
