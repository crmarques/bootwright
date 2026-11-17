package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const (
	sideObserved         = "observed"
	sideApplied          = "applied"
	sideAbsent           = "absent"
	sideUnknown          = "unknown"
	sidePartial          = "partial"
	sidePresent          = "present"
	devNull              = "/dev/null"
	diffContextLines     = 3
	diffDiffersStatus    = 3
	diffNotComparedLimit = 12
)

type diffResult struct {
	Context   diffContext      `json:"context"`
	Operation diffOperation    `json:"operation"`
	Input     string           `json:"input"`
	Adds      bool             `json:"adds,omitempty"`
	Plan      diffPlan         `json:"plan"`
	Observed  bool             `json:"observed"`
	Blocks    diffBlocks       `json:"blocks"`
	Objects   []diffObject     `json:"objects"`
	Files     []diffFileOut    `json:"files"`
	Summary   diffSummary      `json:"summary"`
	Outdated  []outdatedClient `json:"outdatedClients,omitempty"`
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
	Path        string      `json:"path"`
	Difference  string      `json:"difference"`
	Plan        string      `json:"plan"`
	Blocks      []string    `json:"blocks"`
	Realization string      `json:"realization"`
	Real        diffReal    `json:"real"`
	Desired     diffDesired `json:"desired"`
}

type diffReal struct {
	Source      string          `json:"source"`
	State       string          `json:"state"`
	Path        string          `json:"path"`
	Document    json.RawMessage `json:"document"`
	NotCompared []string        `json:"notCompared"`
	Notes       []string        `json:"notes"`
	Reason      string          `json:"reason"`
	Remedy      string          `json:"remedy"`
}

type diffDesired struct {
	Document json.RawMessage `json:"document"`
}

type diffFileOut struct {
	Desired string   `json:"desired"`
	Real    string   `json:"real"`
	Objects []string `json:"objects"`
	Patch   []string `json:"patch"`
}

type diffSummary struct {
	None      int `json:"none"`
	Changed   int `json:"changed"`
	Added     int `json:"added"`
	Removed   int `json:"removed"`
	Unmanaged int `json:"unmanaged"`
	Unknown   int `json:"unknown"`
	Applied   int `json:"applied"`
}

type diffRow struct {
	object  lifecycle.ObjectDiff
	real    json.RawMessage
	desired json.RawMessage
}

type diffOptions struct {
	json, exitCode         bool
	outputDir, tool        string
	yamlEncode, jsonEncode catalogEncoder
	comparison             ComparisonService
	style                  PresentationOptions
}

type catalogEncoder func(context.Context, api.Catalog) ([]byte, error)

func writeDiffStatus(ctx context.Context, out io.Writer, result *lifecycle.DiffResult, options diffOptions) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, encodingFailure(ctx, err)
	}
	if options.yamlEncode == nil || (options.json && options.jsonEncode == nil) {
		return 0, &resultFailure{"runtime.encode", "effective state encoder is not configured", 1}
	}
	rows, err := diffRows(ctx, result, options.json, options.yamlEncode, options.jsonEncode)
	if err != nil {
		return 0, err
	}
	source := diffSource(result)
	files, err := diffFiles(ctx, rows, source, options.yamlEncode)
	if err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, encodingFailure(ctx, err)
	}
	status := 0
	if options.exitCode && lifecycle.SummaryDiffers(result.Summary) {
		status = diffDiffersStatus
	}
	var directory string
	if options.outputDir != "" {
		if directory, err = writeDiffTrees(ctx, options, diffTree(result, rows, files, source)); err != nil {
			return 0, err
		}
	}
	if options.json {
		return status, writeEnvelope(out, commandEnvelope{
			SchemaVersion: "v1alpha1", Command: "diff", OK: status == 0, ExitCode: status,
			Result: displayDiff(result, rows, files), Diagnostics: displayDiagnostics(result.Warnings), Logs: []string{},
		})
	}
	text := diffText(result, rows, files, source, options.style)
	if directory != "" {
		text += "Wrote " + escapeDisplayLine(directory) + "\n"
	}
	if _, err = io.WriteString(out, text); err != nil {
		return status, err
	}
	if options.tool != "" {
		err = runDiffTool(ctx, options, diffTree(result, rows, files, source), files, source)
	}
	return status, err
}

func diffSource(result *lifecycle.DiffResult) string {
	if result.Observed {
		return "real"
	}
	return "applied"
}

func diffRows(ctx context.Context, result *lifecycle.DiffResult, jsonMode bool, yamlEncode, jsonEncode catalogEncoder) ([]diffRow, error) {
	rows := make([]diffRow, 0, len(result.Objects))
	for _, object := range result.Objects {
		if _, _, err := objectDocuments(ctx, object, yamlEncode); err != nil {
			return nil, encodingFailure(ctx, err)
		}
		row := diffRow{object: object}
		if jsonMode && lifecycle.DifferenceDiffers(object.Difference) {
			var err error
			if row.real, row.desired, err = jsonDocuments(ctx, object, jsonEncode); err != nil {
				return nil, encodingFailure(ctx, err)
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
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

func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func displayDiff(result *lifecycle.DiffResult, rows []diffRow, files []diffFile) diffResult {
	objects := make([]diffObject, 0, len(rows))
	for _, row := range rows {
		object, side := row.object, row.object.Real
		objects = append(objects, diffObject{
			Kind: escapeDisplayLine(string(object.Kind)), Name: escapeDisplayLine(object.Name), Path: escapeDisplayLine(diffDesiredPath(object)),
			Difference: string(object.Difference), Plan: escapeDisplayLine(object.Plan), Blocks: displayLines(object.Blocks),
			Realization: escapeDisplayLine(string(object.Realization)),
			Real: diffReal{
				Source: escapeDisplayLine(side.Source), State: escapeDisplayLine(side.State), Path: escapeDisplayLine(diffRealPath(object)), Document: row.real,
				NotCompared: displayLines(side.NotCompared), Notes: displayLines(side.Notes),
				Reason: escapeDisplayLine(side.Reason), Remedy: escapeDisplayLine(side.Remedy),
			},
			Desired: diffDesired{Document: row.desired},
		})
	}
	operation, plan := result.Operation, result.Plan
	return diffResult{
		Context: diffContext{Name: escapeDisplayLine(result.Context.Name), Revision: escapeDisplayLine(result.Context.Revision)},
		Operation: diffOperation{
			ID: escapeDisplayLine(operation.ID), Verb: escapeDisplayLine(operation.Verb), State: escapeDisplayLine(operation.State),
			Apply: escapeDisplayLine(operation.Apply), Revision: escapeDisplayLine(operation.Revision), Applied: escapeDisplayLine(operation.Applied),
		},
		Input: escapeDisplayLine(result.Input), Adds: result.Adds,
		Plan: diffPlan{
			State: escapeDisplayLine(plan.State), Reason: escapeDisplayLine(plan.Reason),
			Changed: plan.Changed, Added: plan.Added, Removed: plan.Removed, Rebuilt: displayLines(plan.Rebuilt),
		},
		Observed: result.Observed,
		Blocks:   diffBlocks{Observed: result.Blocks.Observed, Unknown: result.Blocks.Unknown, Unprojected: result.Blocks.Unprojected},
		Objects:  objects,
		Files:    diffFileResults(files, rows),
		Summary:  diffSummary(result.Summary),
		Outdated: displayOutdatedClients(result.OutdatedClients),
	}
}
