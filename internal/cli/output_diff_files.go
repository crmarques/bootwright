package cli

import (
	"cmp"
	"context"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type diffFile struct {
	path           string
	desired, other []byte
	rows           []int
	patch          []string
}

func (f diffFile) listed() bool { return len(f.rows) != 0 }

type diffSides struct {
	desired, other []api.Object
	rows           []int
}

func diffListed(row diffRow) bool {
	return row.difference != differenceNone || row.object.Real.State == sidePartial
}

func diffFallback(object lifecycle.ObjectDiff) string {
	for _, element := range []string{string(object.Kind), object.Name} {
		if element == "" || element == "." || element == ".." || strings.ContainsAny(element, "/\\\x00") {
			return ""
		}
	}
	return string(object.Kind) + "-" + object.Name + ".yaml"
}

func diffPlacement(object lifecycle.ObjectDiff) (desired, other string, shown *api.Object) {
	fallback := diffFallback(object)
	if object.Desired != nil {
		desired = cmp.Or(object.Path, fallback)
	}
	switch object.Real.State {
	case sidePresent, sidePartial:
		shown, other = object.Real.Object, cmp.Or(object.Real.Path, object.Path, fallback)
	case sideUnknown:
		shown, other = object.Desired, desired
	}
	if shown == nil {
		other = ""
	}
	return desired, other, shown
}

func diffDesiredPath(object lifecycle.ObjectDiff) string {
	if object.Desired == nil {
		return ""
	}
	return object.Path
}

func diffRealPath(object lifecycle.ObjectDiff) string {
	if (object.Real.State != sidePresent && object.Real.State != sidePartial) || object.Real.Object == nil {
		return ""
	}
	return object.Real.Path
}

func diffFiles(ctx context.Context, rows []diffRow, source string, encode catalogEncoder) ([]diffFile, error) {
	sides := map[string]*diffSides{}
	side := func(path string) *diffSides {
		if sides[path] == nil {
			sides[path] = &diffSides{}
		}
		return sides[path]
	}
	for index, row := range rows {
		desiredPath, otherPath, shown := diffPlacement(row.object)
		home := cmp.Or(row.object.Path, diffFallback(row.object))
		if (row.object.Desired != nil && desiredPath == "") || (shown != nil && otherPath == "") || (diffListed(row) && desiredPath == "" && otherPath == "" && home == "") {
			return nil, &resultFailure{"runtime.internal", "the object " + escapeDisplayLine(string(row.object.Kind)+"/"+row.object.Name) + " has no file path and its name makes no clean file name", 1}
		}
		if desiredPath != "" {
			side(desiredPath).desired = append(side(desiredPath).desired, *row.object.Desired)
		}
		if otherPath != "" {
			side(otherPath).other = append(side(otherPath).other, *shown)
		}
		if !diffListed(row) {
			continue
		}
		for _, path := range slices.Compact([]string{desiredPath, otherPath}) {
			if path != "" {
				side(path).rows = append(side(path).rows, index)
			}
		}
		if desiredPath == "" && otherPath == "" {
			side(home).rows = append(side(home).rows, index)
		}
	}
	paths := make([]string, 0, len(sides))
	for path := range sides {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	files := make([]diffFile, 0, len(paths))
	for _, path := range paths {
		file := diffFile{path: path, rows: sides[path].rows}
		var err error
		if file.desired, err = encodeFile(ctx, sides[path].desired, encode); err != nil {
			return nil, encodingFailure(ctx, err)
		}
		if file.other, err = encodeFile(ctx, sides[path].other, encode); err != nil {
			return nil, encodingFailure(ctx, err)
		}
		if file.listed() {
			file.patch = diffFilePatch(file, source)
		}
		files = append(files, file)
	}
	return files, nil
}

func encodeFile(ctx context.Context, objects []api.Object, encode catalogEncoder) ([]byte, error) {
	if len(objects) == 0 {
		return nil, nil
	}
	data, err := encode(ctx, api.NewCatalog(objects))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		return nil, &resultFailure{"runtime.encode", "effective state encoder returned an invalid representation", 1}
	}
	return data, nil
}

func diffFilePatch(file diffFile, source string) []string {
	hunks := unifiedHunks(splitLines(file.desired), splitLines(file.other), diffContextLines)
	if len(hunks) == 0 {
		return nil
	}
	oldLabel, newLabel := "desired/"+file.path, source+"/"+file.path
	if file.desired == nil {
		oldLabel = devNull
	}
	if file.other == nil {
		newLabel = devNull
	}
	return formatUnified(oldLabel, newLabel, hunks)
}

func diffFileResults(files []diffFile, rows []diffRow) []diffFileOut {
	results := []diffFileOut{}
	for _, file := range files {
		if !file.listed() {
			continue
		}
		result := diffFileOut{Objects: []string{}, Patch: displayLines(file.patch)}
		if file.desired != nil {
			result.Desired = escapeDisplayLine(file.path)
		}
		if file.other != nil {
			result.Real = escapeDisplayLine(file.path)
		}
		for _, index := range file.rows {
			object := rows[index].object
			result.Objects = append(result.Objects, escapeDisplayLine(string(object.Kind)+"/"+object.Name))
		}
		results = append(results, result)
	}
	return results
}
