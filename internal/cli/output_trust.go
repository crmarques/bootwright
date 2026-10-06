package cli

import (
	"context"
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
	if !report.Presented {
		text.section("")
		writeTrustTable(&text, report.Hosts)
	}
	return text.writeTo(out)
}

// TrustPlanPresenter writes the evaluated host-key plan before confirmation,
// so an operator compares every fingerprint before authorizing its record.
type TrustPlanPresenter struct{ out io.Writer }

func NewTrustPlanPresenter(out io.Writer) *TrustPlanPresenter {
	return &TrustPlanPresenter{out: out}
}

func (p *TrustPlanPresenter) PresentTrustPlan(ctx context.Context, report enrollment.Report) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil || p.out == nil {
		return &trustOutputFailure{}
	}
	var text display
	text.headline("", "Host-key trust plan for context "+report.Context+": "+trustChecked(&report)+
		", "+strconv.Itoa(report.Pending)+" pending")
	text.section("")
	writeTrustTable(&text, report.Hosts)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := text.writeTo(p.out); err != nil {
		return &trustOutputFailure{}
	}
	return nil
}

type trustOutputFailure struct{}

func (*trustOutputFailure) Error() string { return "host-key trust plan output failed" }

func writeTrustTable(text *display, hosts []enrollment.HostReport) {
	rows := make([][]string, 0, len(hosts))
	for _, host := range hosts {
		rows = append(rows, []string{
			host.Machine, displayValue(trustEndpoint(host)),
			host.Action, displayValue(host.KeyType),
			displayValue(trustDetail(host)),
		})
	}
	text.table([]string{"MACHINE", "ADDRESS", "ACTION", "KEY", "FINGERPRINT"}, rows)
}

// trustHeadline reports what was recorded rather than what was examined, so a
// dry run can never read as though it had written something.
func trustHeadline(report *enrollment.Report) string {
	checked := trustChecked(report)
	if report.DryRun {
		return checked + ", " + strconv.Itoa(report.Pending) + " pending; nothing was recorded"
	}
	if report.Recorded == 0 {
		return checked + "; trust is unchanged"
	}
	return checked + ", " + strconv.Itoa(report.Recorded) + " recorded"
}

// trustChecked counts the selected Machines alone: a removal names the record
// of a Machine the context no longer declares, which nothing checked.
func trustChecked(report *enrollment.Report) string {
	checked := 0
	for _, host := range report.Hosts {
		if host.Action != enrollment.ActionRemove {
			checked++
		}
	}
	return strconv.Itoa(checked) + " machine(s) checked"
}

func trustEndpoint(host enrollment.HostReport) string {
	return endpointToken(host.Address, host.Port)
}

func endpointToken(address string, port int) string {
	if address == "" {
		return ""
	}
	if port == 0 || port == 22 {
		return address
	}
	return "[" + address + "]:" + strconv.Itoa(port)
}

// trustDetail shows the fingerprint an operator compares, for a supersede the
// key and the endpoint it replaces, and for a removal the key removed and why,
// so the change is visible in the result itself.
func trustDetail(host enrollment.HostReport) string {
	switch host.Action {
	case enrollment.ActionSkip:
		return host.Reason
	case enrollment.ActionRemove:
		return host.Fingerprint + " (" + host.Reason + ")"
	}
	changed := host.PreviousFingerprint != "" && host.PreviousFingerprint != host.Fingerprint
	previous := endpointToken(host.PreviousAddress, host.PreviousPort)
	switch {
	case changed && previous != "":
		return host.Fingerprint + " (was " + host.PreviousFingerprint + " at " + previous + ")"
	case previous != "":
		return host.Fingerprint + " (unchanged; was trusted at " + previous + ")"
	case changed:
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
	PreviousAddress     string `json:"previousAddress,omitempty"`
	PreviousPort        int    `json:"previousPort,omitempty"`
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
			PreviousAddress:     escapeDisplayLine(host.PreviousAddress),
			PreviousPort:        host.PreviousPort,
			Reason:              escapeDisplayLine(host.Reason),
		})
	}
	return trustResult{
		Context: escapeDisplayLine(report.Context), DryRun: report.DryRun,
		Pending: report.Pending, Recorded: report.Recorded, Hosts: rows,
	}
}
