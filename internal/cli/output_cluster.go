package cli

import (
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/environment/inspection"
	"github.com/crmarques/bootwright/internal/secrets"
)

var dashboardFingerprint = regexp.MustCompile(`\A[0-9a-f]{64}\z`)

type clusterListPresentation struct {
	Context  string                   `json:"context"`
	Clusters []clusterRefPresentation `json:"clusters"`
}

type clusterRefPresentation struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type clusterInfoPresentation struct {
	Context  string                  `json:"context"`
	Clusters []containerPresentation `json:"clusters"`
	Storage  []storagePresentation   `json:"storage"`
}

type accessCommandPresentation struct {
	Command string `json:"command"`
	Status  string `json:"status"`
	Node    string `json:"node,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type containerPresentation struct {
	Name           string                      `json:"name"`
	Kind           string                      `json:"kind"`
	AccessCommands []accessCommandPresentation `json:"accessCommands"`
}

type storagePresentation struct {
	Name           string                      `json:"name"`
	Kind           string                      `json:"kind"`
	AccessCommands []accessCommandPresentation `json:"accessCommands"`
	*managedCephPresentation
}

type managedCephPresentation struct {
	Management  string                 `json:"management"`
	Release     string                 `json:"release"`
	FSID        string                 `json:"fsid,omitempty"`
	Image       *imagePresentation     `json:"image,omitempty"`
	Monitors    []monitorPresentation  `json:"monitors,omitempty"`
	AdminNode   string                 `json:"adminNode,omitempty"`
	CephCommand string                 `json:"cephCommand,omitempty"`
	Status      string                 `json:"status"`
	Reason      string                 `json:"reason,omitempty"`
	Remedy      string                 `json:"remedy,omitempty"`
	Dashboard   *dashboardPresentation `json:"dashboard,omitempty"`
}

type dashboardPresentation struct {
	Status            string                     `json:"status"`
	Reason            string                     `json:"reason,omitempty"`
	User              string                     `json:"user"`
	URLs              []dashboardURLPresentation `json:"urls"`
	CertificateSHA256 string                     `json:"certificateSHA256"`
	PasswordValue     string                     `json:"passwordValue,omitempty"`
}

type dashboardURLPresentation struct {
	Host string `json:"host"`
	URL  string `json:"url"`
	Role string `json:"role"`
}

type imagePresentation struct {
	Reference string `json:"reference"`
	Digest    string `json:"digest"`
	Platform  string `json:"platform"`
}

type monitorPresentation struct {
	Host    string `json:"host"`
	FQDN    string `json:"fqdn"`
	Address string `json:"address"`
}

func (clusterListPresentation) documentedResult() {}
func (clusterInfoPresentation) documentedResult() {}

var accessStatuses = []string{inspection.StatusNotApplicable, inspection.StatusNotImplemented, inspection.StatusUnavailable, inspection.StatusReady}

func validAccessCommands(commands []inspection.AccessCommand) bool {
	wantOrder := []string{inspection.CommandExec, inspection.CommandKubeconfig, inspection.CommandKubectl, inspection.CommandOC, inspection.CommandRsh}
	if len(commands) != len(wantOrder) {
		return false
	}
	for index, command := range commands {
		if command.Command != wantOrder[index] || !slices.Contains(accessStatuses, command.Status) || (command.Status == inspection.StatusReady) != (command.Reason == "") {
			return false
		}
	}
	return true
}

func validClusterList(result *inspection.ListResult) bool {
	if result == nil || result.Context == "" {
		return false
	}
	return !slices.ContainsFunc(result.Clusters, func(row inspection.ClusterRef) bool {
		return row.Name == "" || row.Kind != inspection.KindContainerCluster && row.Kind != inspection.KindStorageCluster
	})
}

func validClusterInfo(result *inspection.InfoResult) bool {
	if result == nil || result.Context == "" {
		return false
	}
	for _, cluster := range result.Clusters {
		if cluster.Name == "" || cluster.Kind != inspection.KindContainerCluster || !validAccessCommands(cluster.AccessCommands) {
			return false
		}
	}
	for _, cluster := range result.Storage {
		if cluster.Name == "" || cluster.Kind != inspection.KindStorageCluster || !validAccessCommands(cluster.AccessCommands) || !validManagedCeph(cluster.Ceph) {
			return false
		}
	}
	return true
}

func validManagedCeph(ceph *inspection.ManagedCeph) bool {
	if ceph == nil {
		return true
	}
	if ceph.Dashboard != nil && !validDashboard(ceph.Dashboard) {
		return false
	}
	switch ceph.Status {
	case inspection.StatusReady:
		return ceph.FSID != "" && ceph.Image != nil && len(ceph.Monitors) != 0 && ceph.AdminNode != "" && ceph.CephCommand != "" && ceph.Reason == "" && ceph.Remedy == "" &&
			ceph.Dashboard != nil
	case inspection.StatusUnavailable:
		return ceph.Reason != "" && ceph.Remedy != "" && ceph.FSID == "" && ceph.Image == nil && len(ceph.Monitors) == 0 && ceph.CephCommand == "" &&
			(ceph.Dashboard == nil || ceph.Dashboard.Revealed)
	}
	return false
}

func validDashboard(d *inspection.Dashboard) bool {
	if d.User == "" || d.CertificateSHA256 != "" && !dashboardFingerprint.MatchString(d.CertificateSHA256) || d.Warn && !d.Revealed {
		return false
	}
	if d.Status == inspection.DashboardReady && (d.Reason != "" || len(d.URLs) == 0 || d.CertificateSHA256 == "" || d.Warn) ||
		d.Status == inspection.DashboardUnproved && d.Reason == "" || d.Status != inspection.DashboardReady && d.Status != inspection.DashboardUnproved {
		return false
	}
	for index, url := range d.URLs {
		role := inspection.DashboardStandby
		if index == 0 {
			role = inspection.DashboardActive
		}
		if url.Host == "" || url.Role != role || !strings.HasPrefix(url.URL, "https://") {
			return false
		}
	}
	if !d.Revealed {
		return len(d.Password.Parts()) == 0
	}
	value, present := d.Password.Part(secrets.ValuePart)
	defer clear(value)
	return present && len(value) > 0 && len(value) <= 128 && slices.Equal(d.Password.Parts(), []secrets.Part{secrets.ValuePart})
}

func dashboardWarnings(result *inspection.InfoResult) []diagnostic {
	var warnings []diagnostic
	for _, cluster := range result.Storage {
		if cluster.Ceph != nil && cluster.Ceph.Dashboard != nil && cluster.Ceph.Dashboard.Warn {
			warnings = append(warnings, diagnostic{Severity: "warning", Code: "access.unproved", Message: "the dashboard administrator password of StorageCluster " +
				cluster.Name + " was kept from an apply that never proved the dashboard reachable, so the access it grants was not proved"})
		}
	}
	return warnings
}

func clearDashboards(result *inspection.InfoResult) {
	for _, cluster := range result.Storage {
		if cluster.Ceph != nil && cluster.Ceph.Dashboard != nil {
			cluster.Ceph.Dashboard.Password.Clear()
		}
	}
}

func displayDashboard(d *inspection.Dashboard) *dashboardPresentation {
	if d == nil {
		return nil
	}
	shown := &dashboardPresentation{
		Status: escapeDisplayLine(d.Status), Reason: escapeDisplayLine(d.Reason), User: escapeDisplayLine(d.User),
		URLs: []dashboardURLPresentation{}, CertificateSHA256: escapeDisplayLine(d.CertificateSHA256),
	}
	for _, url := range d.URLs {
		shown.URLs = append(shown.URLs, dashboardURLPresentation{Host: escapeDisplayLine(url.Host), URL: escapeDisplayLine(url.URL), Role: escapeDisplayLine(url.Role)})
	}
	if d.Revealed {
		value, _ := d.Password.Part(secrets.ValuePart)
		shown.PasswordValue = escapeDisplayLine(string(value))
		clear(value)
	}
	return shown
}

func displayAccessCommands(commands []inspection.AccessCommand) []accessCommandPresentation {
	rows := make([]accessCommandPresentation, 0, len(commands))
	for _, command := range commands {
		rows = append(rows, accessCommandPresentation{
			Command: escapeDisplayLine(command.Command), Status: escapeDisplayLine(command.Status),
			Node: escapeDisplayLine(command.Node), Reason: escapeDisplayLine(command.Reason),
		})
	}
	return rows
}

func displayManagedCeph(ceph *inspection.ManagedCeph) *managedCephPresentation {
	if ceph == nil {
		return nil
	}
	shown := &managedCephPresentation{
		Management: escapeDisplayLine(ceph.Management), Release: escapeDisplayLine(ceph.Release), FSID: escapeDisplayLine(ceph.FSID),
		AdminNode: escapeDisplayLine(ceph.AdminNode), CephCommand: escapeDisplayLine(ceph.CephCommand),
		Status: escapeDisplayLine(ceph.Status), Reason: escapeDisplayLine(ceph.Reason), Remedy: escapeDisplayLine(ceph.Remedy),
		Dashboard: displayDashboard(ceph.Dashboard),
	}
	if ceph.Image != nil {
		shown.Image = &imagePresentation{
			Reference: escapeDisplayLine(ceph.Image.Reference), Digest: escapeDisplayLine(ceph.Image.Digest), Platform: escapeDisplayLine(ceph.Image.Platform),
		}
	}
	for _, monitor := range ceph.Monitors {
		shown.Monitors = append(shown.Monitors, monitorPresentation{
			Host: escapeDisplayLine(monitor.Host), FQDN: escapeDisplayLine(monitor.FQDN), Address: escapeDisplayLine(monitor.Address),
		})
	}
	return shown
}

func displayClusterInfo(result *inspection.InfoResult) clusterInfoPresentation {
	shown := clusterInfoPresentation{
		Context: escapeDisplayLine(result.Context), Clusters: []containerPresentation{}, Storage: []storagePresentation{},
	}
	for _, cluster := range result.Clusters {
		shown.Clusters = append(shown.Clusters, containerPresentation{
			Name: escapeDisplayLine(cluster.Name), Kind: escapeDisplayLine(cluster.Kind), AccessCommands: displayAccessCommands(cluster.AccessCommands),
		})
	}
	for _, cluster := range result.Storage {
		shown.Storage = append(shown.Storage, storagePresentation{
			Name: escapeDisplayLine(cluster.Name), Kind: escapeDisplayLine(cluster.Kind),
			AccessCommands: displayAccessCommands(cluster.AccessCommands), managedCephPresentation: displayManagedCeph(cluster.Ceph),
		})
	}
	return shown
}

func writeClusterResult(out, errOut io.Writer, command string, result commandResult, jsonMode bool) (bool, error) {
	if command == "cluster list" && validClusterList(result.clusterList) {
		return true, writeClusterList(out, command, result.clusterList, jsonMode)
	}
	if result.clusterInfo != nil {
		defer clearDashboards(result.clusterInfo)
	}
	if command == "cluster info" && validClusterInfo(result.clusterInfo) {
		return true, writeClusterInfo(out, errOut, command, result.clusterInfo, jsonMode)
	}
	return false, nil
}

func writeClusterList(out io.Writer, command string, result *inspection.ListResult, jsonMode bool) error {
	if jsonMode {
		shown := clusterListPresentation{Context: escapeDisplayLine(result.Context), Clusters: []clusterRefPresentation{}}
		for _, row := range result.Clusters {
			shown.Clusters = append(shown.Clusters, clusterRefPresentation{Name: escapeDisplayLine(row.Name), Kind: escapeDisplayLine(row.Kind)})
		}
		return writeEnvelope(out, commandEnvelope{
			SchemaVersion: "v1alpha1", Command: escapeDisplayLine(command), OK: true, ExitCode: 0,
			Result: shown, Diagnostics: []diagnostic{}, Logs: []string{},
		})
	}
	var text display
	if len(result.Clusters) == 0 {
		text.headline("OK", "No clusters are selected")
		return text.writeTo(out)
	}
	rows := make([][]string, 0, len(result.Clusters))
	for _, row := range result.Clusters {
		rows = append(rows, []string{row.Name, row.Kind})
	}
	text.resourceTable([]string{"NAME", "KIND"}, rows, tableStyle{Kind: "cluster", Identity: 0})
	return text.writeTo(out)
}

func writeClusterInfo(out, errOut io.Writer, command string, result *inspection.InfoResult, jsonMode bool) error {
	warnings := dashboardWarnings(result)
	if jsonMode {
		return writeEnvelope(out, commandEnvelope{
			SchemaVersion: "v1alpha1", Command: escapeDisplayLine(command), OK: true, ExitCode: 0,
			Result: displayClusterInfo(result), Diagnostics: append([]diagnostic{}, displayDiagnostics(warnings)...), Logs: []string{},
		})
	}
	if len(warnings) != 0 {
		if err := writeHumanDiagnostics(errOut, displayDiagnostics(warnings)); err != nil {
			return err
		}
	}
	var text display
	if len(result.Clusters)+len(result.Storage) == 0 {
		text.headline("OK", "No clusters are selected")
		return text.writeTo(out)
	}
	full := presentationFor(out).Detail == "full"
	records := 0
	for _, cluster := range result.Clusters {
		if records > 0 {
			text.separate()
		}
		records++
		text.resourceHeading("cluster", cluster.Name, "")
		text.subsection("Topology")
		text.mutedFields(field{Label: "Type", Value: cluster.Kind})
		writeAccessCommands(&text, result.Context, cluster.Name, cluster.AccessCommands, full)
	}
	for _, cluster := range result.Storage {
		status := ""
		if cluster.Ceph != nil {
			status = cluster.Ceph.Status
		}
		if records > 0 {
			text.separate()
		}
		records++
		text.resourceHeading("cluster", cluster.Name, status)
		text.subsection("Topology")
		topology := []field{{Label: "Type", Value: cluster.Kind}}
		if cluster.Ceph != nil {
			topology = append(topology, managedCephTopology(cluster.Ceph)...)
		}
		text.mutedFields(topology...)
		if cluster.Ceph != nil {
			writeManagedCeph(&text, result.Context, cluster.Name, cluster.Ceph, cluster.AccessCommands, full)
		} else {
			writeAccessCommands(&text, result.Context, cluster.Name, cluster.AccessCommands, full)
		}
	}
	return text.writeTo(out)
}

func managedCephTopology(ceph *inspection.ManagedCeph) []field {
	fields := []field{{Label: "Management", Value: ceph.Management}, {Label: "Release", Value: ceph.Release}}
	if ceph.Status == inspection.StatusReady {
		fields = append(fields, field{Label: "Admin node", Value: ceph.AdminNode})
	}
	return fields
}

func writeManagedCeph(text *display, contextName, cluster string, ceph *inspection.ManagedCeph, commands []inspection.AccessCommand, full bool) {
	writeAccessCommands(text, contextName, cluster, commands, full)
	if ceph.Status == inspection.StatusReady {
		monitors := make([]field, 0, len(ceph.Monitors))
		for index, monitor := range ceph.Monitors {
			label := ""
			if index == 0 {
				label = "Monitors"
			}
			monitors = append(monitors, field{Label: label, Value: monitor.Host + "  " + monitor.FQDN + "  " + monitor.Address})
		}
		text.mutedFields(monitors...)
		text.mutedVerbatimFields(field{Label: "Ceph command", Value: ceph.CephCommand})
	}
	if ceph.Dashboard != nil {
		text.mutedFields(dashboardAccessFields(ceph.Dashboard)...)
	}
	text.subsection("Evidence")
	fields := []field{{Label: "Status", Value: ceph.Status}}
	if ceph.Status != inspection.StatusReady {
		fields = append(fields, field{Label: "Reason", Value: ceph.Reason}, field{Label: "Next", Value: ceph.Remedy})
	} else if full {
		fields = append(fields, field{Label: "FSID", Value: ceph.FSID}, field{Label: "Image", Value: ceph.Image.Reference},
			field{Label: "Digest", Value: ceph.Image.Digest}, field{Label: "Platform", Value: ceph.Image.Platform})
	}
	text.mutedFields(fields...)
	if ceph.Dashboard != nil {
		d := ceph.Dashboard
		password := "hidden; rerun with --secrets --name " + cluster
		if d.Revealed {
			value, _ := d.Password.Part(secrets.ValuePart)
			password = string(value)
			clear(value)
		}
		text.subsection("Custody")
		text.mutedFields(field{Label: "User", Value: d.User}, field{Label: "Password", Value: password})
	}
}

func dashboardAccessFields(d *inspection.Dashboard) []field {
	if d == nil {
		return nil
	}
	fields := []field{}
	for index, url := range d.URLs {
		label, note := "", "(standby "+url.Host+"; redirects to the active MGR)"
		if index == 0 {
			label, note = "Dashboard", "(active MGR "+url.Host+" at last observation)"
		}
		fields = append(fields, field{Label: label, Value: url.URL + "  " + note})
	}
	status := d.Status
	if d.Reason != "" {
		status += ": " + d.Reason
	}
	fields = append(fields, field{Label: "Dashboard status", Value: status})
	if d.CertificateSHA256 != "" {
		fields = append(fields, field{Label: "Certificate SHA-256", Value: colonHex(d.CertificateSHA256) + "  (self-signed; compare before accepting the browser warning)"})
	}
	return fields
}

func colonHex(digest string) string {
	pairs := make([]string, 0, len(digest)/2)
	for index := 0; index+1 < len(digest); index += 2 {
		pairs = append(pairs, strings.ToUpper(digest[index:index+2]))
	}
	return strings.Join(pairs, ":")
}

func writeAccessCommands(text *display, contextName, cluster string, commands []inspection.AccessCommand, full bool) {
	text.subsection("Access")
	for _, command := range commands {
		text.mutedFields(field{Label: command.Command, Value: command.Status})
		switch command.Status {
		case inspection.StatusReady:
			text.mutedVerbatimLines([]string{accessTemplate(contextName, cluster, command)})
		case inspection.StatusNotApplicable:
			if full {
				text.mutedLines([]string{command.Reason})
			}
		default:
			text.mutedLines([]string{command.Reason})
			if full {
				text.mutedVerbatimLines([]string{"template (" + command.Status + "): " + accessTemplate(contextName, cluster, command)})
			}
		}
	}
}

func accessTemplate(contextName, cluster string, command inspection.AccessCommand) string {
	base := "bootwright " + command.Command + " --context " + contextName + " --name " + cluster
	switch command.Command {
	case inspection.CommandExec, inspection.CommandRsh:
		node := command.Node
		if node == "" {
			node = "<node>"
		}
		base += " --node " + node
		if command.Command == inspection.CommandExec {
			base += " -- <command>..."
		}
	case inspection.CommandOC, inspection.CommandKubectl:
		base += " -- <command>..."
	}
	return base
}
