package cli

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const (
	differenceNone    = "none"
	differenceChanged = "changed"
	differenceAdded   = "added"
	differenceRemoved = "removed"
	differenceUnknown = "unknown"
	sideObserved      = "observed"
	sideApplied       = "applied"
	sideAbsent        = "absent"
	sideUnknown       = "unknown"
	sidePartial       = "partial"
	sidePresent       = "present"
	devNull           = "/dev/null"
	diffContextLines  = 3
	diffDiffersStatus = 3
)

type diffResult struct {
	Context   diffContext   `json:"context"`
	Operation diffOperation `json:"operation"`
	Input     string        `json:"input"`
	Plan      diffPlan      `json:"plan"`
	Observed  bool          `json:"observed"`
	Blocks    diffBlocks    `json:"blocks"`
	Objects   []diffObject  `json:"objects"`
	Summary   diffSummary   `json:"summary"`
}

func (diffResult) documentedResult() {}

type diffContext struct {
	Name     string `json:"name"`
	Revision string `json:"revision"`
}

type diffOperation struct {
	ID       string `json:"id"`
	Verb     string `json:"verb"`
	State    string `json:"state"`
	Apply    string `json:"apply"`
	Revision string `json:"revision"`
	Applied  string `json:"applied"`
}

type diffPlan struct {
	State   string   `json:"state"`
	Reason  string   `json:"reason"`
	Changed int      `json:"changed"`
	Added   int      `json:"added"`
	Removed int      `json:"removed"`
	Rebuilt []string `json:"rebuilt"`
}

type diffBlocks struct {
	Observed    int `json:"observed"`
	Unknown     int `json:"unknown"`
	Unprojected int `json:"unprojected"`
}

type diffObject struct {
	Kind        string      `json:"kind"`
	Name        string      `json:"name"`
	Difference  string      `json:"difference"`
	Plan        string      `json:"plan"`
	Blocks      []string    `json:"blocks"`
	Realization string      `json:"realization"`
	Real        diffReal    `json:"real"`
	Desired     diffDesired `json:"desired"`
	Patch       []string    `json:"patch"`
}

type diffReal struct {
	Source      string          `json:"source"`
	State       string          `json:"state"`
	Document    json.RawMessage `json:"document"`
	NotCompared []string        `json:"notCompared"`
	Notes       []string        `json:"notes"`
	Reason      string          `json:"reason"`
	Remedy      string          `json:"remedy"`
}

type diffDesired struct {
	Document json.RawMessage `json:"document"`
}

type diffSummary struct {
	None    int `json:"none"`
	Changed int `json:"changed"`
	Added   int `json:"added"`
	Removed int `json:"removed"`
	Unknown int `json:"unknown"`
	Applied int `json:"applied"`
}

type diffRow struct {
	object     lifecycle.ObjectDiff
	difference string
	patch      []string
	real       json.RawMessage
	desired    json.RawMessage
}

type catalogEncoder func(context.Context, api.Catalog) ([]byte, error)

func writeDiff(ctx context.Context, out io.Writer, result *lifecycle.DiffResult, jsonMode bool, yamlEncode, jsonEncode catalogEncoder) error {
	_, err := writeDiffStatus(ctx, out, result, jsonMode, false, yamlEncode, jsonEncode)
	return err
}

func writeDiffStatus(ctx context.Context, out io.Writer, result *lifecycle.DiffResult, jsonMode, exitCode bool, yamlEncode, jsonEncode catalogEncoder) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, encodingFailure(ctx, err)
	}
	if yamlEncode == nil || (jsonMode && jsonEncode == nil) {
		return 0, &resultFailure{"runtime.encode", "effective state encoder is not configured", 1}
	}
	rows, err := diffRows(ctx, result, jsonMode, yamlEncode, jsonEncode)
	if err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, encodingFailure(ctx, err)
	}
	status := 0
	if exitCode && diffDiffers(diffSummaryOf(rows, result.Observed)) {
		status = diffDiffersStatus
	}
	if jsonMode {
		return status, writeEnvelope(out, commandEnvelope{
			SchemaVersion: "v1alpha1", Command: "diff", OK: status == 0, ExitCode: status,
			Result: displayDiff(result, rows), Diagnostics: []diagnostic{}, Logs: []string{},
		})
	}
	_, err = io.WriteString(out, diffText(result, rows))
	return status, err
}

func diffDiffers(summary diffSummary) bool {
	return summary.Changed+summary.Added+summary.Removed+summary.Unknown != 0
}

func diffRows(ctx context.Context, result *lifecycle.DiffResult, jsonMode bool, yamlEncode, jsonEncode catalogEncoder) ([]diffRow, error) {
	rows := make([]diffRow, 0, len(result.Objects))
	for _, object := range result.Objects {
		object.Real = documented(object.Real)
		real, desired, err := objectDocuments(ctx, object, yamlEncode)
		if err != nil {
			return nil, encodingFailure(ctx, err)
		}
		row := diffRow{object: object, difference: difference(object, real, desired)}
		if differs(row.difference) {
			row.patch = diffPatch(object, row.difference, real, desired)
			if jsonMode {
				if row.real, row.desired, err = jsonDocuments(ctx, object, jsonEncode); err != nil {
					return nil, encodingFailure(ctx, err)
				}
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func documented(side lifecycle.RealSide) lifecycle.RealSide {
	if (side.State != sidePresent && side.State != sidePartial) || side.Object != nil {
		return side
	}
	side.State, side.Reason = sideUnknown, cmp.Or(side.Reason, "the real side holds no document to show")
	return side
}

func differs(difference string) bool {
	return difference != differenceNone && difference != differenceUnknown
}

func objectDocuments(ctx context.Context, object lifecycle.ObjectDiff, encode catalogEncoder) (real, desired []byte, err error) {
	if real, err = encodeOne(ctx, object.Real.Object, encode); err != nil {
		return nil, nil, err
	}
	desired, err = encodeOne(ctx, object.Desired, encode)
	return real, desired, err
}

func encodeOne(ctx context.Context, object *api.Object, encode catalogEncoder) ([]byte, error) {
	if object == nil {
		return nil, nil
	}
	data, err := encode(ctx, api.NewCatalog([]api.Object{*object}))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return nil, &resultFailure{"runtime.encode", "effective state encoder returned an invalid representation", 1}
	}
	return data, nil
}

func jsonDocuments(ctx context.Context, object lifecycle.ObjectDiff, encode catalogEncoder) (real, desired json.RawMessage, err error) {
	for index, side := range []*api.Object{object.Real.Object, object.Desired} {
		if side == nil {
			continue
		}
		data, err := encode(ctx, api.NewCatalog([]api.Object{*side}))
		if err != nil {
			return nil, nil, err
		}
		if len(data) < 4 || data[0] != '[' || !bytes.HasSuffix(data, []byte("]\n")) {
			return nil, nil, &resultFailure{"runtime.encode", "effective state encoder returned an invalid representation", 1}
		}
		document := json.RawMessage(bytes.Clone(data[1 : len(data)-2]))
		if index == 0 {
			real = document
		} else {
			desired = document
		}
	}
	return real, desired, nil
}

func difference(object lifecycle.ObjectDiff, real, desired []byte) string {
	switch {
	case object.Real.State == sideUnknown:
		return differenceUnknown
	case object.Real.State == sideAbsent && object.Desired != nil:
		return differenceAdded
	case object.Desired == nil && object.Real.State != sideAbsent:
		return differenceRemoved
	case bytes.Equal(real, desired):
		return differenceNone
	}
	return differenceChanged
}

func diffPatch(object lifecycle.ObjectDiff, kind string, real, desired []byte) []string {
	identity := string(object.Kind) + "/" + object.Name
	oldLabel, newLabel := sidePrefix(object.Real.Source)+"/"+identity, "desired/"+identity
	if kind == differenceAdded || object.Real.Object == nil {
		oldLabel = devNull
	}
	if object.Desired == nil {
		newLabel = devNull
	}
	return formatUnified(oldLabel, newLabel, unifiedHunks(splitLines(real), splitLines(desired), diffContextLines))
}

func sidePrefix(source string) string {
	if source == sideObserved {
		return "real"
	}
	return "applied"
}

func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func diffSummaryOf(rows []diffRow, observed bool) diffSummary {
	var summary diffSummary
	for _, row := range rows {
		switch row.difference {
		case differenceChanged:
			summary.Changed++
		case differenceAdded:
			summary.Added++
		case differenceRemoved:
			summary.Removed++
		case differenceUnknown:
			summary.Unknown++
		default:
			summary.None++
		}
		if observed && row.object.Real.Source == sideApplied && (row.object.Real.State == sidePresent || row.object.Real.State == sidePartial) {
			summary.Applied++
		}
	}
	return summary
}

func displayDiff(result *lifecycle.DiffResult, rows []diffRow) diffResult {
	objects := make([]diffObject, 0, len(rows))
	for _, row := range rows {
		object, side := row.object, row.object.Real
		objects = append(objects, diffObject{
			Kind: escapeDisplayLine(string(object.Kind)), Name: escapeDisplayLine(object.Name),
			Difference: row.difference, Plan: escapeDisplayLine(object.Plan), Blocks: displayLines(object.Blocks),
			Realization: escapeDisplayLine(string(object.Realization)),
			Real: diffReal{
				Source: escapeDisplayLine(side.Source), State: escapeDisplayLine(side.State), Document: row.real,
				NotCompared: displayLines(side.NotCompared), Notes: displayLines(side.Notes),
				Reason: escapeDisplayLine(side.Reason), Remedy: escapeDisplayLine(side.Remedy),
			},
			Desired: diffDesired{Document: row.desired},
			Patch:   displayLines(row.patch),
		})
	}
	operation, plan := result.Operation, result.Plan
	return diffResult{
		Context: diffContext{Name: escapeDisplayLine(result.Context.Name), Revision: escapeDisplayLine(result.Context.Revision)},
		Operation: diffOperation{
			ID: escapeDisplayLine(operation.ID), Verb: escapeDisplayLine(operation.Verb), State: escapeDisplayLine(operation.State),
			Apply: escapeDisplayLine(operation.Apply), Revision: escapeDisplayLine(operation.Revision), Applied: escapeDisplayLine(operation.Applied),
		},
		Input: escapeDisplayLine(result.Input),
		Plan: diffPlan{
			State: escapeDisplayLine(plan.State), Reason: escapeDisplayLine(plan.Reason),
			Changed: plan.Changed, Added: plan.Added, Removed: plan.Removed, Rebuilt: displayLines(plan.Rebuilt),
		},
		Observed: result.Observed,
		Blocks:   diffBlocks{Observed: result.Blocks.Observed, Unknown: result.Blocks.Unknown, Unprojected: result.Blocks.Unprojected},
		Objects:  objects,
		Summary:  diffSummaryOf(rows, result.Observed),
	}
}

func diffText(result *lifecycle.DiffResult, rows []diffRow) string {
	var text strings.Builder
	for _, line := range diffHeader(result) {
		text.WriteString(escapeDisplayLine(line) + "\n")
	}
	for _, row := range rows {
		if row.difference == differenceNone && row.object.Real.State != sidePartial {
			continue
		}
		text.WriteString("\n")
		for _, line := range diffObjectLines(row) {
			text.WriteString(line + "\n")
		}
		for _, line := range row.patch {
			text.WriteString(escapeDisplayLine(line) + "\n")
		}
	}
	text.WriteString("\n" + escapeDisplayLine(diffSummaryLine(result, diffSummaryOf(rows, result.Observed), len(rows))) + "\n")
	return text.String()
}

func diffHeader(result *lifecycle.DiffResult) []string {
	lines := []string{
		"Context:  " + result.Context.Name + " at input revision " + result.Context.Revision,
		"Against:  " + diffAgainst(result),
		"Input:    " + diffInput(result),
		"Plan:     " + diffPlanLine(result.Plan),
	}
	if rebuilt := result.Plan.Rebuilt; len(rebuilt) != 0 {
		line := "Build:    this executable plans " + strconv.Itoa(len(rebuilt)) + " " + plural(len(rebuilt), "block", "blocks") +
			" of " + result.Operation.Apply + " differently from the build that froze " + plural(len(rebuilt), "it", "them")
		if result.Observed {
			line += "; --observe does not read " + plural(len(rebuilt), "it", "them")
		}
		lines = append(lines, line)
	}
	return lines
}

func diffAgainst(result *lifecycle.DiffResult) string {
	operation := result.Operation
	switch {
	case operation.ID == "":
		return "nothing; this context holds no operation, so nothing is realized"
	case result.Observed:
		return "what apply " + operation.Apply + " (" + operation.State + ") realized, read live: " + strconv.Itoa(result.Blocks.Observed) +
			" blocks observed, " + strconv.Itoa(result.Blocks.Unknown) + " unknown, " + strconv.Itoa(result.Blocks.Unprojected) + " not projected"
	case operation.Verb == "destroy":
		return "destroy " + operation.ID + " (" + operation.State + ") of apply " + operation.Apply + ", as its records prove; no host was contacted"
	}
	return "apply " + operation.ID + " (" + operation.State + ") of input revision " + operation.Revision + ", as its records prove; no host was contacted"
}

func diffInput(result *lifecycle.DiffResult) string {
	name, operation := result.Context.Name, result.Operation
	next := "; bootwright status --context " + name + " names the next step"
	switch {
	case result.Input == "none":
		return "not applied yet" + next
	case result.Input == "changed":
		return "changed since " + operation.Apply + "; bootwright apply refuses it until bootwright destroy --context " + name + " takes back what that apply owns"
	case operation.Verb == "destroy":
		return "unchanged since " + operation.Apply + ", which the removal in progress takes back" + next
	}
	return "unchanged since " + operation.Apply + next
}

func diffPlanLine(plan lifecycle.DiffPlan) string {
	switch plan.State {
	case "same":
		return "this executable plans the same blocks"
	case "differs":
		return "this executable plans " + strconv.Itoa(plan.Changed) + " " + plural(plan.Changed, "block", "blocks") + " differently, " +
			strconv.Itoa(plan.Added) + " more and " + strconv.Itoa(plan.Removed) + " fewer"
	case "unplanned":
		return "this executable no longer plans the input: " + plan.Reason
	}
	return "no apply froze a plan to compare with"
}

func diffObjectLines(row diffRow) []string {
	object, side := row.object, row.object.Real
	identity := string(object.Kind) + "/" + object.Name
	lines := []string{escapeDisplayLine("diff " + sidePrefix(side.Source) + "/" + identity + " desired/" + identity)}
	plan := "none"
	if len(object.Blocks) != 0 {
		plan = cmp.Or(object.Plan, "unplanned") + " (" + strings.Join(object.Blocks, ", ") + "); realized: " + cmp.Or(string(object.Realization), "none")
	}
	lines = append(lines, "# plan: "+plan)
	if len(side.NotCompared) != 0 {
		lines = append(lines, "# not compared: "+strings.Join(side.NotCompared, ", "))
	}
	for _, note := range side.Notes {
		lines = append(lines, "# "+note)
	}
	if side.Reason != "" {
		lines = append(lines, "# "+side.Reason)
	}
	if side.Remedy != "" {
		lines = append(lines, "# remedy: "+side.Remedy)
	}
	for index := 1; index < len(lines); index++ {
		lines[index] = escapeDisplayLine(lines[index])
	}
	return lines
}

func diffSummaryLine(result *lifecycle.DiffResult, summary diffSummary, objects int) string {
	differing := summary.Changed + summary.Added + summary.Removed + summary.Unknown
	if !diffDiffers(summary) {
		line := "No differences: " + strconv.Itoa(objects) + " " + plural(objects, "object", "objects") + " compared"
		if result.Observed {
			line += ", " + strconv.Itoa(summary.Applied) + " as applied"
		}
		return line + "."
	}
	line := strconv.Itoa(differing) + " " + plural(differing, "object differs", "objects differ") + ": " + strconv.Itoa(summary.Changed) + " changed, " + strconv.Itoa(summary.Added) + " added, " +
		strconv.Itoa(summary.Removed) + " removed, " + strconv.Itoa(summary.Unknown) + " unknown; " + strconv.Itoa(summary.None) + " unchanged"
	if result.Observed {
		line += "; " + strconv.Itoa(summary.Applied) + " compared as applied"
	}
	return line
}
