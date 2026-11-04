package cli

import (
	"context"
	"strings"

	artifactrendering "github.com/crmarques/bootwright/internal/nativeartifacts/rendering"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const diffNotesFile = "notes.txt"

type ComparisonService interface {
	WriteTrees(ctx context.Context, directory string, files []artifactrendering.TreeFile) (string, error)
	RunTool(ctx context.Context, program string, files []artifactrendering.TreeFile, pairs []artifactrendering.ComparisonPair) error
}

func diffTree(result *lifecycle.DiffResult, rows []diffRow, files []diffFile, source string) []artifactrendering.TreeFile {
	var tree []artifactrendering.TreeFile
	for _, file := range files {
		if file.desired != nil {
			tree = append(tree, artifactrendering.TreeFile{Path: "desired/" + file.path, Data: file.desired})
		}
		if file.other != nil {
			tree = append(tree, artifactrendering.TreeFile{Path: source + "/" + file.path, Data: file.other})
		}
	}
	for _, marker := range result.Markers.Desired {
		tree = append(tree, artifactrendering.TreeFile{Path: "desired/" + marker.Path(), Data: marker.Bytes()})
	}
	for _, marker := range result.Markers.Applied {
		tree = append(tree, artifactrendering.TreeFile{Path: source + "/" + marker.Path(), Data: marker.Bytes()})
	}
	return append(tree, artifactrendering.TreeFile{Path: diffNotesFile, Data: []byte(diffNotes(result, rows, files))})
}

func diffNotes(result *lifecycle.DiffResult, rows []diffRow, files []diffFile) string {
	var notes strings.Builder
	for _, line := range diffHeaderLines(result) {
		notes.WriteString(line + "\n")
	}
	for _, file := range files {
		for _, line := range diffNoteLines(file, rows) {
			notes.WriteString(line + "\n")
		}
	}
	return notes.String()
}

func writeDiffTrees(ctx context.Context, options diffOptions, tree []artifactrendering.TreeFile) (string, error) {
	if options.comparison == nil {
		return "", &resultFailure{"runtime.internal", "comparison files are not configured", 1}
	}
	return options.comparison.WriteTrees(ctx, options.outputDir, tree)
}

func runDiffTool(ctx context.Context, options diffOptions, tree []artifactrendering.TreeFile, files []diffFile, source string) error {
	if options.comparison == nil {
		return &resultFailure{"runtime.internal", "comparison files are not configured", 1}
	}
	var pairs []artifactrendering.ComparisonPair
	for _, file := range files {
		if !file.listed() {
			continue
		}
		pair := artifactrendering.ComparisonPair{Desired: "desired/" + file.path, Other: source + "/" + file.path}
		if file.desired == nil {
			tree = append(tree, artifactrendering.TreeFile{Path: pair.Desired})
		}
		if file.other == nil {
			tree = append(tree, artifactrendering.TreeFile{Path: pair.Other})
		}
		pairs = append(pairs, pair)
	}
	return options.comparison.RunTool(ctx, options.tool, tree, pairs)
}
