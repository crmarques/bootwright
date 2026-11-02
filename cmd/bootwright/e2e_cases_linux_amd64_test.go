//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type e2eRender struct {
	name string
	sets []string
}

var e2eRenders = map[string][]e2eRender{
	"lab-rhel":                       {{name: "default"}},
	"lab-sno":                        {{name: "default"}},
	"lab-baremetal-rehearsal":        {{name: "default"}},
	"lab-network-variants":           {{name: "default"}},
	"lab-3-nodes-ceph-host-variants": {{name: "default"}},
	"lab-3-nodes-ocp":                {{name: "metallb off", sets: []string{"addons.metallb=false"}}},
	"lab-3-nodes-ocp-features":       {{name: "mirror off", sets: []string{"mirror.enabled=false"}}},
	"lab-5-nodes-ceph-stretch": {
		{name: "default"},
		{name: "file off", sets: []string{"ceph.services.file=false"}},
		{name: "object off", sets: []string{"ceph.services.object=false"}},
	},
	"lab-3-nodes-ceph": {
		{name: "default"},
		{name: "block off", sets: []string{"ceph.services.block=false"}},
		{name: "file off", sets: []string{"ceph.services.file=false"}},
		{name: "object off", sets: []string{"ceph.services.object=false"}},
		{name: "nfs off", sets: []string{"ceph.services.nfs=false"}},
		{name: "smb off", sets: []string{"ceph.services.smb=false"}},
		{name: "observability off", sets: []string{"ceph.services.observability=false"}},
		{name: "nvmeof on", sets: []string{"ceph.services.nvmeof=true"}},
		{name: "every group off", sets: []string{
			"ceph.services.block=false", "ceph.services.file=false", "ceph.services.object=false",
			"ceph.services.nfs=false", "ceph.services.smb=false", "ceph.services.observability=false",
		}},
	},
}

func e2eCaseNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("..", "..", "e2e"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		for _, required := range []string{"values.yaml", "templates"} {
			if _, err := os.Stat(filepath.Join("..", "..", "e2e", entry.Name(), required)); err != nil {
				t.Fatalf("e2e/%s is not a case: %v", entry.Name(), err)
			}
		}
		names = append(names, entry.Name())
	}
	return names
}

func e2eInputDirectories(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var directories []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".yaml") {
			return []string{root}
		}
		if entry.IsDir() {
			directories = append(directories, filepath.Join(root, entry.Name()))
		}
	}
	if len(directories) == 0 {
		t.Fatalf("%s rendered no input", root)
	}
	return directories
}

func TestEveryE2ECaseRendersToValidInput(t *testing.T) {
	if _, err := exec.LookPath("helm"); err != nil {
		t.Fatalf("rendering the e2e cases needs helm on PATH: %v", err)
	}
	names := e2eCaseNames(t)
	for name := range e2eRenders {
		if !slices.Contains(names, name) {
			t.Fatalf("e2eRenders names %q, which is not a case under e2e/", name)
		}
	}
	for _, name := range names {
		if len(e2eRenders[name]) == 0 {
			t.Fatalf("e2e/%s has no entry in e2eRenders; add the renders its runbook selects", name)
		}
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "e2e", "render.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		for _, render := range e2eRenders[name] {
			t.Run(name+"/"+render.name, func(t *testing.T) {
				output := filepath.Join(t.TempDir(), "input")
				arguments := []string{name, "--out-dir", output}
				for _, set := range render.sets {
					arguments = append(arguments, "--set", set)
				}
				if combined, err := exec.Command(script, arguments...).CombinedOutput(); err != nil {
					t.Fatalf("render.sh %s: %v\n%s", strings.Join(arguments, " "), err, combined)
				}
				for _, directory := range e2eInputDirectories(t, output) {
					var out, errOut bytes.Buffer
					code := run(context.Background(), []string{"validate", "-f", directory, "--output", "json"}, &out, &errOut)
					if code != 0 {
						var envelope struct {
							Diagnostics []struct {
								Code    string
								Message string
								Source  struct{ Path, Kind, Name string }
							}
						}
						if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
							t.Fatalf("validate %s exited %d: %s %s", directory, code, out.String(), errOut.String())
						}
						for _, diagnostic := range envelope.Diagnostics {
							t.Errorf("validate %s: %s %s/%s: %s", filepath.Base(directory), diagnostic.Code, diagnostic.Source.Kind, diagnostic.Source.Name, diagnostic.Message)
						}
						t.Fatalf("validate %s exited %d", directory, code)
					}
				}
			})
		}
	}
}
