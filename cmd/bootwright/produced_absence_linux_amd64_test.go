//go:build linux && amd64

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/storage/ceph"
	"github.com/crmarques/bootwright/internal/workspace/contextfs"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const (
	clientKeyToken  = "AQBx57l5Y2FuYXJ5LWNsaWVudC1rZXk9PQ=="
	clientCanary    = "[client.rbd-canary]\n\tkey = " + clientKeyToken + "\n"
	dashboardCanary = "Dx57-L5_canary.Pw9-Qz4_Rt7.Kv2-Hm8"
)

func cephExampleContext(t *testing.T, name string) (cli.Services, *contextfs.Store) {
	t.Helper()
	return productionExampleContext(t, name, blockFileExample)
}

func productionExampleContext(t *testing.T, name, example string) (cli.Services, *contextfs.Store) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "state")
	repository := testRepository(root)
	deps := testContextWiring(t, root)
	deps.Repository, deps.Workspace, deps.Trust = repository, repository, repository
	deps.Lifecycle = lifecycleDependencies{
		Workspace: repository,
		Inputs:    contexts.Inputs{Repository: repository, Selection: deps.Selection},
		Host:      testLifecycleHost{}, Guard: testLifecycleGuard{}, Selection: deps.Selection,
		Presenter: testLifecyclePresenter{}, Confirmer: deps.Confirmer,
		Capabilities: buildCapabilities(systemClock{}, exampleControllerPorts(t), exampleMediaRecords{}),
	}
	services := assembleServices(deps)
	input := filepath.Join(t.TempDir(), name)
	base, err := filepath.Abs(examplePath(t, example))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range exampleDirectory(t, example).Files {
		relative, err := filepath.Rel(base, file.Path())
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(input, filepath.Dir(relative)), 0700); err != nil {
			t.Fatal(err)
		}
		addSecretInput(t, filepath.Join(input, filepath.Dir(relative)), filepath.Base(relative), string(file.Bytes()))
	}
	contextRun(t, services, 0, "context", "init", "--name", name, "--input-dir", input)
	return services, repository
}

func producedForms(values ...string) []string {
	var forms []string
	for _, value := range values {
		raw := []byte(value)
		forms = append(forms, value, base64.StdEncoding.EncodeToString(raw), base64.RawStdEncoding.EncodeToString(raw),
			base64.URLEncoding.EncodeToString(raw), hex.EncodeToString(raw), strings.ToUpper(hex.EncodeToString(raw)))
	}
	return forms
}

func outputRun(t *testing.T, services cli.Services, args ...string) (int, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := runServices(t.Context(), args, &out, &errOut, services)
	return code, out.String() + errOut.String()
}

func TestNoProducedMaterialInOrdinaryOutput(t *testing.T) {
	services, repository := cephExampleContext(t, "lab-ceph")
	produceThroughLentArea(t, services, repository, "lab-ceph", "ceph-client-rbd-canary", "client", clientCanary)
	produceThroughLentArea(t, services, repository, "lab-ceph", ceph.DashboardBlock("ceph-lab"), ceph.DashboardOutput, dashboardCanary)
	if value, found := readProduced(t, services, "lab-ceph", "ceph-client-rbd-canary", "client"); !found || value != clientCanary {
		t.Fatalf("the client canary reads %q (%t)", value, found)
	}
	if value, found := readProduced(t, services, "lab-ceph", ceph.DashboardBlock("ceph-lab"), ceph.DashboardOutput); !found || value != dashboardCanary {
		t.Fatalf("the dashboard canary reads %q (%t)", value, found)
	}
	if revealed, _ := contextRun(t, services, 0, "cluster", "info", "--context", "lab-ceph", "--name", "ceph-lab", "--secrets", "--output", "json"); !strings.Contains(revealed, dashboardCanary) {
		t.Fatalf("an explicit reveal = %q, so the scan below proves nothing", revealed)
	}
	forms := producedForms(clientCanary, dashboardCanary, clientKeyToken)
	for _, run := range []struct {
		code int
		args []string
	}{
		{0, []string{"status", "--context", "lab-ceph"}},
		{0, []string{"status", "--context", "lab-ceph", "--output", "json"}},
		{0, []string{"status", "--context", "lab-ceph", "--output", "yaml"}},
		{0, []string{"plan", "--context", "lab-ceph"}},
		{0, []string{"plan", "--context", "lab-ceph", "--output", "json"}},
		{0, []string{"diff", "--context", "lab-ceph"}},
		{0, []string{"diff", "--context", "lab-ceph", "--output", "json"}},
		{0, []string{"cluster", "list", "--context", "lab-ceph"}},
		{0, []string{"cluster", "info", "--context", "lab-ceph"}},
		{0, []string{"cluster", "info", "--context", "lab-ceph", "--detail", "full"}},
		{0, []string{"cluster", "info", "--context", "lab-ceph", "--output", "json"}},
		{0, []string{"cluster", "info", "--context", "lab-ceph", "--output", "yaml"}},
		{0, []string{"cluster", "info", "--context", "lab-ceph", "--name", "ceph-lab"}},
	} {
		code, written := outputRun(t, services, run.args...)
		if code != run.code || written == "" {
			t.Fatalf("%v exited %d, want %d, writing %q", run.args, code, run.code, written)
		}
		for _, form := range forms {
			if strings.Contains(written, form) {
				t.Fatalf("%v carries %q: %s", run.args, form, written)
			}
		}
	}
	listed, _ := contextRun(t, services, 0, "cluster", "info", "--context", "lab-ceph", "--name", "ceph-lab", "--output", "json")
	var envelope struct {
		Result struct {
			Storage []struct {
				Resources []map[string]string `json:"resources"`
			} `json:"storage"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(listed), &envelope); err != nil || len(envelope.Result.Storage) != 1 {
		t.Fatalf("cluster info = %s (%v)", listed, err)
	}
	pending := func(kind, name string) map[string]string {
		return map[string]string{"kind": kind, "name": name, "status": "pending"}
	}
	want := []map[string]string{
		pending("CephPlacementPolicy", "ssd-hosts"), pending("CephPool", "cephfs-data"), pending("CephPool", "cephfs-metadata"),
		pending("CephPool", "rbd-data"), pending("CephRBDImage", "rbd-disk"), pending("CephClient", "rbd-canary"),
		pending("CephFilesystem", "files"), pending("CephSubvolume", "docs"),
	}
	if got := envelope.Result.Storage[0].Resources; !reflect.DeepEqual(got, want) {
		t.Fatalf("resources = %v, want %v", got, want)
	}
}
