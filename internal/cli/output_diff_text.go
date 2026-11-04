package cli

import (
	"cmp"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const (
	styleBold  = "1"
	styleCyan  = "36"
	styleRed   = "31"
	styleGreen = "32"
	styleDim   = "2"
)

func diffText(result *lifecycle.DiffResult, rows []diffRow, files []diffFile, source string, style PresentationOptions) string {
	var text strings.Builder
	for _, line := range diffHeaderLines(result) {
		text.WriteString(line + "\n")
	}
	for _, file := range files {
		if !file.listed() {
			continue
		}
		text.WriteString("\n")
		for _, line := range diffFileLines(file, rows, source, style) {
			text.WriteString(line + "\n")
		}
	}
	partial := 0
	for _, row := range rows {
		if row.object.Real.State == sidePartial {
			partial++
		}
	}
	text.WriteString("\n" + escapeDisplayLine(diffSummaryLine(result, diffSummaryOf(rows, result.Observed), len(rows), partial)) + "\n")
	return text.String()
}

func diffHeaderLines(result *lifecycle.DiffResult) []string {
	return displayLines(diffHeader(result))
}

func diffFileLines(file diffFile, rows []diffRow, source string, style PresentationOptions) []string {
	lines := []string{presentationStyle(escapeDisplayLine("diff desired/"+file.path+" "+source+"/"+file.path), styleBold, style)}
	for _, line := range diffNoteLines(file, rows) {
		lines = append(lines, presentationStyle(line, styleDim, style))
	}
	for index, line := range displayLines(file.patch) {
		lines = append(lines, styleDiffPatchLine(line, index, style))
	}
	return lines
}

func styleDiffPatchLine(line string, index int, style PresentationOptions) string {
	switch {
	case index < 2:
		return presentationStyle(line, styleBold, style)
	case strings.HasPrefix(line, "@@"):
		return presentationStyle(line, styleCyan, style)
	case strings.HasPrefix(line, "-"):
		return presentationStyle(line, styleRed, style)
	case strings.HasPrefix(line, "+"):
		return presentationStyle(line, styleGreen, style)
	}
	return line
}

func diffNoteLines(file diffFile, rows []diffRow) []string {
	var lines []string
	for _, index := range file.rows {
		lines = append(lines, diffObjectLines(rows[index])...)
	}
	return lines
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
	case result.Input == "changed" && result.Adds:
		return "changed since " + operation.Apply + "; bootwright apply adds it to that apply without running any of its blocks"
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
	prefix := "# " + string(object.Kind) + "/" + object.Name + ": "
	plan := "none"
	if len(object.Blocks) != 0 {
		plan = cmp.Or(object.Plan, "unplanned") + " (" + strings.Join(object.Blocks, ", ") + "); realized: " + cmp.Or(string(object.Realization), "none")
	}
	lines := []string{prefix + "plan: " + plan}
	if len(side.NotCompared) != 0 {
		lines = append(lines, prefix+"not compared: "+diffNotCompared(side.NotCompared))
	}
	for _, note := range side.Notes {
		lines = append(lines, prefix+note)
	}
	if side.Reason != "" {
		lines = append(lines, prefix+side.Reason)
	}
	if side.Remedy != "" {
		lines = append(lines, prefix+"remedy: "+side.Remedy)
	}
	return displayLines(lines)
}

func diffNotCompared(paths []string) string {
	if len(paths) <= diffNotComparedLimit {
		return strings.Join(paths, ", ")
	}
	return strings.Join(paths[:diffNotComparedLimit], ", ") + " and " + strconv.Itoa(len(paths)-diffNotComparedLimit) + " more"
}

func diffSummaryLine(result *lifecycle.DiffResult, summary diffSummary, objects, partial int) string {
	differing := summary.Changed + summary.Added + summary.Removed + summary.Unmanaged + summary.Unknown
	var line string
	if !diffDiffers(summary) {
		line = "No differences"
		if partial > 0 {
			line += " in compared fields"
		}
		line += ": " + strconv.Itoa(objects) + " " + plural(objects, "object", "objects") + " compared"
		if result.Observed {
			line += ", " + strconv.Itoa(summary.Applied) + " as applied"
		}
		if partial == 0 {
			return line + "."
		}
	} else {
		line = strconv.Itoa(differing) + " " + plural(differing, "object differs", "objects differ") + ": " + strconv.Itoa(summary.Changed) + " changed, " + strconv.Itoa(summary.Added) + " added, " +
			strconv.Itoa(summary.Removed) + " removed, " + strconv.Itoa(summary.Unmanaged) + " unmanaged, " + strconv.Itoa(summary.Unknown) + " unknown; " + strconv.Itoa(summary.None) + " unchanged"
		if result.Observed {
			line += "; " + strconv.Itoa(summary.Applied) + " compared as applied"
		}
	}
	if partial > 0 {
		line += "; " + strconv.Itoa(partial) + " " + plural(partial, "object", "objects") + " partially realized."
	}
	return line
}
