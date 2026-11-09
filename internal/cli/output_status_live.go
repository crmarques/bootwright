package cli

import (
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type statusLive struct {
	Block      string `json:"block"`
	Attempt    int    `json:"attempt"`
	Resolution int    `json:"resolution"`
	Output     string `json:"output"`
	Present    bool   `json:"present"`
	Bytes      int64  `json:"bytes"`
}

type statusPath struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Present bool   `json:"present"`
}

func displayLive(live []lifecycle.LiveBlock) []statusLive {
	shown := make([]statusLive, 0, len(live))
	for _, block := range live {
		shown = append(shown, statusLive{Block: escapeDisplayLine(block.Block), Attempt: block.Attempt, Resolution: block.Resolution,
			Output: escapeDisplayLine(block.Output), Present: block.Present, Bytes: block.Bytes})
	}
	return shown
}

func displayPaths(paths []lifecycle.ContextPath) []statusPath {
	if paths == nil {
		return nil
	}
	shown := make([]statusPath, 0, len(paths))
	for _, entry := range paths {
		shown = append(shown, statusPath{Kind: escapeDisplayLine(entry.Kind), Path: escapeDisplayLine(entry.Path), Present: entry.Present})
	}
	return shown
}

func writeStatusLive(text *display, summary *lifecycle.LifecycleSummary, contextName string) {
	if len(summary.Live) == 0 {
		return
	}
	operation := "contexts/" + contextName + "/operations/" + summary.Operation + "/"
	rows := make([][]string, 0, len(summary.Live))
	for _, block := range summary.Live {
		attempt := strconv.Itoa(block.Attempt)
		if block.Resolution != 0 {
			attempt += " resolution " + strconv.Itoa(block.Resolution)
		}
		written := "none yet"
		if block.Present {
			written = humanBytes(block.Bytes)
		}
		rows = append(rows, []string{block.Block, attempt, strings.TrimPrefix(block.Output, operation), written})
	}
	text.section("Live")
	text.resourceTable([]string{"Block", "Attempt", "Output", "Written"}, rows, tableStyle{Kind: "block", Identity: 0})
}

func writeStatusPaths(text *display, paths []lifecycle.ContextPath) {
	if len(paths) == 0 {
		return
	}
	rows := make([][]string, 0, len(paths))
	for _, entry := range paths {
		state := "absent"
		if entry.Present {
			state = "present"
		}
		rows = append(rows, []string{entry.Kind, entry.Path, state})
	}
	text.section("Paths")
	text.resourceTable([]string{"Kind", "Path", "State"}, rows, tableStyle{Kind: "path", Identity: 1, States: []int{2}})
}
