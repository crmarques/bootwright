//go:build linux && amd64

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/internal/infrastructureservices"
)

type e2eRenderEntry struct {
	once        sync.Once
	directories []string
	err         error
}

type e2ePlanEntry struct {
	once         sync.Once
	completed    bool
	refusal      string
	reservations []infrastructureservices.HostReservation
}

var e2eRenderCache struct {
	root    string
	lock    sync.Mutex
	renders map[string]*e2eRenderEntry
	plans   map[string]*e2ePlanEntry
	tools   struct {
		once sync.Once
		path string
		err  error
	}
}

func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "bootwright-e2e-renders-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	e2eRenderCache.root = root
	code := m.Run()
	os.RemoveAll(root)
	os.Exit(code)
}

func e2eRenderEntryFor(key string) (*e2eRenderEntry, string) {
	e2eRenderCache.lock.Lock()
	defer e2eRenderCache.lock.Unlock()
	if e2eRenderCache.renders == nil {
		e2eRenderCache.renders = map[string]*e2eRenderEntry{}
	}
	entry, ok := e2eRenderCache.renders[key]
	if !ok {
		entry = &e2eRenderEntry{}
		e2eRenderCache.renders[key] = entry
	}
	return entry, filepath.Join(e2eRenderCache.root, "render-"+strconv.Itoa(len(e2eRenderCache.renders)), "input")
}

// Each distinct render runs helm once per test process; every test that asks
// for it reads the same directories and never writes into them.
func renderE2ECase(t *testing.T, script, name string, render e2eRender) []string {
	t.Helper()
	environment := e2eRenderEnvironment(t)
	entry, output := e2eRenderEntryFor(name + "\x00" + strings.Join(render.sets, "\x00"))
	entry.once.Do(func() {
		entry.directories, entry.err = e2eRenderInto(script, name, render, output, environment)
	})
	if entry.err != nil {
		t.Fatal(entry.err)
	}
	return entry.directories
}

func renderE2ECaseUncached(t *testing.T, script, name string, render e2eRender) []string {
	t.Helper()
	directories, err := e2eRenderInto(script, name, render, filepath.Join(t.TempDir(), "input"), e2eRenderEnvironment(t))
	if err != nil {
		t.Fatal(err)
	}
	return directories
}

func e2eRenderEnvironment(t *testing.T) []string {
	t.Helper()
	tools := &e2eRenderCache.tools
	tools.once.Do(func() { tools.path, tools.err = e2eFixtureRouteTools() })
	if tools.err != nil {
		t.Fatal(tools.err)
	}
	return append(os.Environ(), "PATH="+e2ePath(t, tools.path))
}

func e2eRenderInto(script, name string, render e2eRender, output string, environment []string) ([]string, error) {
	arguments := []string{name, "--out-dir", output}
	for _, set := range render.sets {
		arguments = append(arguments, "--set", set)
	}
	command := exec.Command(script, arguments...)
	command.Env = environment
	if combined, err := command.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("render.sh %s: %v\n%s", strings.Join(arguments, " "), err, combined)
	}
	return e2eInputDirectories(output)
}

func e2eInputDirectories(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var directories []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".yaml") {
			return []string{root}, nil
		}
		if entry.IsDir() {
			directories = append(directories, filepath.Join(root, entry.Name()))
		}
	}
	if len(directories) == 0 {
		return nil, fmt.Errorf("%s rendered no input", root)
	}
	return directories, nil
}

func e2eFixtureRouteTools() (string, error) {
	tools := filepath.Join(e2eRenderCache.root, "route-tools")
	if err := os.MkdirAll(tools, 0o700); err != nil {
		return "", err
	}
	script := `#!/bin/sh
set -eu
if [ "$#" -ne 4 ] || [ "$1" != -4 ] || [ "$2" != route ] || [ "$3" != get ] || [ "$4" != 192.0.2.1 ]; then
    exit 2
fi
printf '%s\n' '192.0.2.1 dev fixture0 src 192.0.2.10'
`
	return tools, os.WriteFile(filepath.Join(tools, "ip"), []byte(script), 0o700)
}

// A failed compile ends the first caller's test; the entry then stays
// incomplete so a later caller fails instead of reading an empty result as a
// pass.
func planE2EInputOnce(t *testing.T, directory string) (string, []infrastructureservices.HostReservation) {
	t.Helper()
	e2eRenderCache.lock.Lock()
	if e2eRenderCache.plans == nil {
		e2eRenderCache.plans = map[string]*e2ePlanEntry{}
	}
	entry, ok := e2eRenderCache.plans[directory]
	if !ok {
		entry = &e2ePlanEntry{}
		e2eRenderCache.plans[directory] = entry
	}
	e2eRenderCache.lock.Unlock()
	entry.once.Do(func() {
		entry.refusal, entry.reservations = planE2EInput(t, directory)
		entry.completed = true
	})
	if !entry.completed {
		t.Fatalf("planning %s failed in an earlier test", directory)
	}
	return entry.refusal, entry.reservations
}
