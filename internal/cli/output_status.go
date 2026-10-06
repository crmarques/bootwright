package cli

import (
	"encoding/json"
	"io"
	"strconv"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type statusResult struct {
	Context         resultContext    `json:"context"`
	SetupChecks     []statusCheck    `json:"setupChecks"`
	Desired         statusDesired    `json:"desired"`
	Clusters        []statusCluster  `json:"clusters"`
	StorageClusters []statusCluster  `json:"storageClusters"`
	Shared          []statusService  `json:"shared"`
	Secrets         statusSecrets    `json:"secrets"`
	NextSteps       []string         `json:"nextSteps"`
	Lifecycle       *statusLifecycle `json:"lifecycle"`
	Contradictions  []string         `json:"contradictions"`
}

func (statusResult) documentedResult() {}

type statusCheck struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type statusDesired struct {
	Revision    string          `json:"revision"`
	Environment string          `json:"environment"`
	Counts      admissionCounts `json:"counts"`
}

type statusCluster struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Status string `json:"status"`
}

type statusService struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Machine string `json:"machine"`
	Status  string `json:"status"`
}

type statusSecrets struct {
	Declared int `json:"declared"`
	Bindings int `json:"bindings"`
}

// statusLifecycle leaves out the registering build and the host log directory,
// which only a person reads.
type statusLifecycle struct {
	Operation string        `json:"operation"`
	Verb      string        `json:"verb"`
	State     string        `json:"state"`
	Next      string        `json:"next"`
	Blocks    []statusBlock `json:"blocks"`
	Logs      []string      `json:"logs"`
}

type statusBlock struct {
	ID          string            `json:"id"`
	Description string            `json:"description"`
	Stage       string            `json:"stage"`
	State       string            `json:"state"`
	Attempts    int               `json:"attempts"`
	Unresolved  *statusUnresolved `json:"unresolved,omitempty"`
}

type statusUnresolved struct {
	Reason string `json:"reason"`
	Remedy string `json:"remedy"`
}

func displayStatus(result *lifecycle.StatusResult) statusResult {
	out := statusResult{
		Context:     resultContext{Name: escapeDisplayLine(result.Context.Name), Mode: escapeDisplayLine(result.Context.Mode)},
		SetupChecks: make([]statusCheck, 0, len(result.SetupChecks)),
		Desired: statusDesired{
			Revision: escapeDisplayLine(result.Desired.Revision), Environment: escapeDisplayLine(result.Desired.Environment),
			Counts: admissionCounts{FilesSeen: result.Desired.Files, ObjectsDecoded: result.Desired.Objects},
		},
		Clusters:        displayStatusClusters(result.Clusters),
		StorageClusters: displayStatusClusters(result.StorageClusters),
		Shared:          make([]statusService, 0, len(result.Shared)),
		Secrets:         statusSecrets{Declared: result.Secrets.Declared, Bindings: result.Secrets.Bindings},
		NextSteps:       displayLines(result.NextSteps),
		Contradictions:  displayLines(result.Contradictions),
	}
	for _, check := range result.SetupChecks {
		out.SetupChecks = append(out.SetupChecks, statusCheck{ID: escapeDisplayLine(check.ID), Status: escapeDisplayLine(check.Status)})
	}
	for _, service := range result.Shared {
		out.Shared = append(out.Shared, statusService{
			Kind: escapeDisplayLine(service.Kind), Name: escapeDisplayLine(service.Name),
			Machine: escapeDisplayLine(service.Machine), Status: escapeDisplayLine(string(service.Status)),
		})
	}
	if summary := result.Lifecycle; summary != nil {
		blocks := make([]statusBlock, 0, len(summary.Blocks))
		for _, block := range summary.Blocks {
			row := statusBlock{
				ID: escapeDisplayLine(block.ID), Description: escapeDisplayLine(block.Description),
				Stage: escapeDisplayLine(block.Stage), State: escapeDisplayLine(block.State), Attempts: block.Attempts,
			}
			if block.Unresolved != nil {
				row.Unresolved = &statusUnresolved{Reason: escapeDisplayLine(block.Unresolved.Reason), Remedy: escapeDisplayLine(block.Unresolved.Remedy)}
			}
			blocks = append(blocks, row)
		}
		out.Lifecycle = &statusLifecycle{
			Operation: escapeDisplayLine(summary.Operation), Verb: escapeDisplayLine(summary.Verb),
			State: escapeDisplayLine(summary.State), Next: escapeDisplayLine(summary.Next),
			Blocks: blocks, Logs: displayLines(summary.Logs),
		}
	}
	return out
}

func displayStatusClusters(clusters []lifecycle.ClusterSummary) []statusCluster {
	out := make([]statusCluster, 0, len(clusters))
	for _, cluster := range clusters {
		out = append(out, statusCluster{
			Name: escapeDisplayLine(cluster.Name), Kind: escapeDisplayLine(cluster.Kind), Status: escapeDisplayLine(string(cluster.Status)),
		})
	}
	return out
}

// writeLifecycleStatus presents the JSON membership in the JSON order. The
// human text passes raw values, because display escapes each one it writes.
func writeLifecycleStatus(out io.Writer, result *lifecycle.StatusResult, jsonMode bool) error {
	if jsonMode {
		encoder := json.NewEncoder(out)
		encoder.SetEscapeHTML(false)
		return encoder.Encode(commandEnvelope{
			SchemaVersion: "v1alpha1", Command: "status", OK: true, ExitCode: 0,
			Result: displayStatus(result), Diagnostics: []diagnostic{}, Logs: []string{},
		})
	}
	var text display
	text.headline("OK", "Context "+result.Context.Name)
	text.section("")
	text.fields(field{Label: "Mode", Value: displayValue(result.Context.Mode)})
	writeStatusSetup(&text, result.SetupChecks)
	writeStatusDesired(&text, result.Desired)
	writeStatusClusters(&text, "Clusters", result.Clusters)
	writeStatusClusters(&text, "Storage clusters", result.StorageClusters)
	writeStatusShared(&text, result.Shared)
	text.section("Secrets")
	text.fields(
		field{Label: "Declared", Value: strconv.Itoa(result.Secrets.Declared)},
		field{Label: "Bindings", Value: strconv.Itoa(result.Secrets.Bindings)},
	)
	if len(result.NextSteps) != 0 {
		text.section("Next steps")
		text.lines(result.NextSteps)
	}
	writeStatusLifecycle(&text, result)
	if len(result.Contradictions) != 0 {
		text.section("Contradictions")
		text.lines(result.Contradictions)
	}
	return text.writeTo(out)
}

// writeStatusSetup labels each check as controller readiness does. A binding
// still pending says what publishes it.
func writeStatusSetup(text *display, checks []lifecycle.SetupCheck) {
	if len(checks) == 0 {
		return
	}
	text.section("Setup")
	rows := make([][]string, 0, len(checks))
	for _, check := range checks {
		row := []string{checkToken(check.Status), controllerActionLabel(check.ID)}
		if check.ID == "controller-binding" && check.Status == "pending" {
			row = append(row, "bound by the first apply")
		}
		rows = append(rows, row)
	}
	text.rows(rows)
}

func writeStatusDesired(text *display, desired lifecycle.DesiredSummary) {
	text.section("Desired")
	text.fields(
		field{Label: "Revision", Value: displayValue(desired.Revision)},
		field{Label: "Environment", Value: displayValue(desired.Environment)},
		field{Label: "Files seen", Value: strconv.Itoa(desired.Files)},
		field{Label: "Objects decoded", Value: strconv.Itoa(desired.Objects)},
	)
}

func writeStatusClusters(text *display, title string, clusters []lifecycle.ClusterSummary) {
	if len(clusters) == 0 {
		return
	}
	text.section(title)
	rows := make([][]string, 0, len(clusters))
	for _, cluster := range clusters {
		rows = append(rows, []string{serviceStatusToken(cluster.Status), cluster.Kind + "/" + cluster.Name})
	}
	text.rows(rows)
}

func writeStatusShared(text *display, services []lifecycle.ServiceSummary) {
	if len(services) == 0 {
		return
	}
	text.section("Shared services")
	rows := make([][]string, 0, len(services))
	for _, service := range services {
		rows = append(rows, []string{serviceStatusToken(service.Status), service.Kind + "/" + service.Name, displayValue(service.Machine)})
	}
	text.rows(rows)
}

// writeStatusLifecycle adds what JSON leaves out: the build that registered
// the operation, which a removal is planned from and a refusal names as its
// remedy, and the host directory of its logs.
func writeStatusLifecycle(text *display, result *lifecycle.StatusResult) {
	summary := result.Lifecycle
	if summary == nil {
		return
	}
	text.section("Lifecycle")
	text.fields(
		field{Label: "Operation", Value: displayValue(summary.Operation)},
		field{Label: "Verb", Value: displayValue(summary.Verb)},
		field{Label: "State", Value: displayValue(summary.State)},
		field{Label: "Next", Value: displayValue(summary.Next)},
	)
	if len(summary.Blocks) != 0 {
		text.section("")
		rows := make([][]string, 0, len(summary.Blocks))
		for _, block := range summary.Blocks {
			rows = append(rows, []string{blockStatusToken(block.State), block.Description})
		}
		text.rows(rows)
	}
	var tail []field
	if summary.Executable != "" {
		tail = append(tail, field{Label: "Registered by", Value: summary.Executable})
	}
	if result.LogLocation != "" {
		tail = append(tail, field{Label: logLocationLabel, Value: result.LogLocation})
	}
	if len(tail) != 0 {
		text.section("")
		text.fields(tail...)
	}
	for _, block := range summary.Blocks {
		if block.Unresolved == nil {
			continue
		}
		text.section("Unresolved " + block.ID)
		text.fields(
			field{Label: "Reason", Value: block.Unresolved.Reason},
			field{Label: "Remedy", Value: block.Unresolved.Remedy},
		)
	}
}

// serviceStatusToken presents a cluster or shared service status. A value
// outside the vocabulary proves nothing, so it never reads as a definite
// failure.
func serviceStatusToken(status lifecycle.RealizationStatus) string {
	switch status {
	case lifecycle.RealizationDone:
		return "[OK]"
	case lifecycle.RealizationPending:
		return "[PENDING]"
	case lifecycle.RealizationFailed:
		return "[FAIL]"
	case lifecycle.RealizationUnsupported:
		return "[SKIPPED]"
	}
	return "[UNKNOWN]"
}
