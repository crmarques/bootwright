//go:build linux && amd64

package venvlocal

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

const interpreterProbe = `import bz2, ctypes, json, os, ssl, sys, venv, zlib
json.dump({"executable": sys.executable, "version": "%d.%d.%d" % sys.version_info[:3],
 "abi": "%d.%d" % sys.version_info[:2], "cacheTag": sys.implementation.cache_tag,
 "libc": os.confstr("CS_GNU_LIBC_VERSION") or "", "stdlib": os.path.dirname(os.__file__),
 "dynload": [p for p in sys.path if os.path.basename(p) == "lib-dynload"]}, sys.stdout)
`

type interpreterReport struct {
	Executable string   `json:"executable"`
	Version    string   `json:"version"`
	ABI        string   `json:"abi"`
	CacheTag   string   `json:"cacheTag"`
	Libc       string   `json:"libc"`
	Stdlib     string   `json:"stdlib"`
	Dynload    []string `json:"dynload"`
}

// Select returns the first qualified interpreter: the receipt's when it still
// is one of a locked ABI, then /usr/bin/python3.X for each locked ABI, newest
// first. Only the versioned path is a candidate, so the path fixes the ABI.
func (a *Adapter) Select(ctx context.Context, preferred string) (prerequisites.PythonInterpreter, error) {
	var candidates, abis []string
	for _, abi := range a.lock.PythonABIs() {
		candidates = append(candidates, filepath.Join(a.bin, "python"+abi))
		abis = append(abis, abi)
	}
	if index := slices.Index(candidates, preferred); index > 0 {
		abi := abis[index]
		candidates = slices.Insert(slices.Delete(candidates, index, index+1), 0, preferred)
		abis = slices.Insert(slices.Delete(abis, index, index+1), 0, abi)
	}
	var refusals []error
	for index, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return prerequisites.PythonInterpreter{}, err
		}
		interpreter, err := a.qualify(ctx, candidate, abis[index])
		if err == nil {
			return interpreter, nil
		}
		refusals = append(refusals, err)
	}
	return prerequisites.PythonInterpreter{}, errors.Join(append([]error{prerequisites.ErrNoPythonInterpreter}, refusals...)...)
}

// qualify walks a candidate and the stdlib and lib-dynload its prefix fixes
// before the probe imports from them, then holds the probe to those.
func (a *Adapter) qualify(ctx context.Context, path, abi string) (prerequisites.PythonInterpreter, error) {
	resolved, err := a.ownedExecutable(path)
	if err != nil {
		return prerequisites.PythonInterpreter{}, err
	}
	stdlib := filepath.Join(filepath.Dir(filepath.Dir(resolved)), "lib64", "python"+abi)
	dynload := filepath.Join(stdlib, "lib-dynload")
	for _, directory := range []string{stdlib, dynload} {
		if err := a.ownedDirectory(directory); err != nil {
			return prerequisites.PythonInterpreter{}, err
		}
	}
	output, err := a.run(ctx, command{
		Path: resolved, Arguments: []string{"-I", "-B", "-c", interpreterProbe},
		Environment: cleanEnvironment(""), Directory: "/",
	})
	if err != nil {
		return prerequisites.PythonInterpreter{}, failure(path + " did not answer the interpreter probe")
	}
	var report interpreterReport
	if canonicaljson.DecodeClosed(output, &report) != nil {
		return prerequisites.PythonInterpreter{}, failure(path + " answered the interpreter probe with something else")
	}
	minor := strings.TrimPrefix(abi, "3.")
	if report.Executable != resolved || report.ABI != abi || report.CacheTag != "cpython-3"+minor || !strings.HasPrefix(report.Version, abi+".") {
		return prerequisites.PythonInterpreter{}, failure(fmt.Sprintf("%s is Python %s (%s), not CPython %s", path, report.Version, report.CacheTag, abi))
	}
	glibc, found := strings.CutPrefix(report.Libc, "glibc ")
	if !found || !atLeast(glibc, a.lock.GlibcFloor()) {
		return prerequisites.PythonInterpreter{}, failure(fmt.Sprintf("%s runs on %q, below the locked wheels' glibc %s", path, report.Libc, a.lock.GlibcFloor()))
	}
	if report.Stdlib != stdlib || !slices.Equal(report.Dynload, []string{dynload}) {
		return prerequisites.PythonInterpreter{}, failure(fmt.Sprintf("%s loads its stdlib from %s and %v, not the walked %s and %s", path, report.Stdlib, report.Dynload, stdlib, dynload))
	}
	return prerequisites.PythonInterpreter{Path: path, ABI: abi, Version: report.Version, Glibc: glibc, Stdlib: []string{stdlib, dynload}}, nil
}

// atLeast compares two dotted numeric versions; anything unparsable is below.
func atLeast(version, floor string) bool {
	have, want := strings.Split(version, "."), strings.Split(floor, ".")
	for index := range max(len(have), len(want)) {
		h, w := 0, 0
		var err error
		if index < len(have) {
			if h, err = strconv.Atoi(have[index]); err != nil {
				return false
			}
		}
		if index < len(want) {
			if w, err = strconv.Atoi(want[index]); err != nil {
				return false
			}
		}
		if h != w {
			return h > w
		}
	}
	return true
}
