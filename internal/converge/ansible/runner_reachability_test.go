package ansible

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRunResultIsUnreachableWithMixedHostEvents(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{name: "initial unreachable", body: `{"host":"step_0","status":"unreachable"}` + "\n" + `{"schemaVersion":1,"status":"terminal","processedHosts":["step_0"],"hosts":{"step_0":{"ok":0,"failed":0,"skipped":0,"unreachable":1}}}` + "\n", want: true},
		{name: "skipped then unreachable", body: `{"host":"step_0","status":"skipped"}` + "\n" + `{"host":"step_0","status":"unreachable"}` + "\n" + `{"schemaVersion":1,"status":"terminal","processedHosts":["step_0"],"hosts":{"step_0":{"ok":0,"failed":0,"skipped":1,"unreachable":1}}}` + "\n", want: true},
		{name: "reachable before unreachable", body: `{"host":"step_0","status":"ok"}` + "\n" + `{"host":"step_0","status":"unreachable"}` + "\n" + `{"schemaVersion":1,"status":"terminal","processedHosts":["step_0"],"hosts":{"step_0":{"ok":1,"failed":0,"skipped":0,"unreachable":1}}}` + "\n", want: true},
		{name: "other host reachable", body: `{"host":"step_0","status":"ok"}` + "\n" + `{"host":"step_1","status":"unreachable"}` + "\n" + `{"schemaVersion":1,"status":"terminal","processedHosts":["step_0","step_1"],"hosts":{"step_0":{"ok":1,"failed":0,"skipped":0,"unreachable":0},"step_1":{"ok":0,"failed":0,"skipped":0,"unreachable":1}}}` + "\n", want: true},
		{name: "diagnostic probe only", body: `{"host":"step_0","status":"probe-unreachable"}` + "\n" + `{"schemaVersion":1,"status":"terminal","processedHosts":["step_0"],"hosts":{"step_0":{"ok":0,"failed":0,"skipped":0,"unreachable":0,"probeUnreachable":1}}}` + "\n"},
		{name: "failure after unreachable", body: `{"host":"step_0","status":"unreachable"}` + "\n" + `{"host":"step_0","status":"failed"}` + "\n" + `{"schemaVersion":1,"status":"terminal","processedHosts":["step_0"],"hosts":{"step_0":{"ok":0,"failed":1,"skipped":0,"unreachable":1}}}` + "\n"},
		{name: "success only", body: `{"host":"step_0","status":"ok"}` + "\n" + `{"schemaVersion":1,"status":"terminal","processedHosts":["step_0"],"hosts":{"step_0":{"ok":1,"failed":0,"skipped":0,"unreachable":0}}}` + "\n"},
		{name: "malformed", body: "not-json\n"},
		{name: "empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), RunResultName)
			if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
				t.Fatalf("write result: %v", err)
			}
			if got := runResultIsUnreachable(path); got != tc.want {
				t.Fatalf("runResultIsUnreachable = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestAppendCallbackEnvEnablesReachabilityResultAndOptionalProfile(t *testing.T) {
	collectionsPath := t.TempDir()
	callbackDir := filepath.Join(collectionsPath, filepath.FromSlash(profileCallbackRelPath))
	if err := os.MkdirAll(callbackDir, 0o700); err != nil {
		t.Fatalf("mkdir callback dir: %v", err)
	}
	t.Setenv(ProfileEnvVar, "")
	if got := callbackEnvValue(appendCallbackEnv(nil, collectionsPath, true), "ANSIBLE_CALLBACKS_ENABLED"); got != runResultCallbackName {
		t.Fatalf("callbacks = %q, want %q", got, runResultCallbackName)
	}
	t.Setenv(ProfileEnvVar, "1")
	want := profileCallbackName + "," + runResultCallbackName
	if got := callbackEnvValue(appendCallbackEnv(nil, collectionsPath, true), "ANSIBLE_CALLBACKS_ENABLED"); got != want {
		t.Fatalf("callbacks = %q, want %q", got, want)
	}
}

func callbackEnvValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if len(entry) >= len(prefix) && entry[:len(prefix)] == prefix {
			return entry[len(prefix):]
		}
	}
	return ""
}

func TestCommandRunnerReturnsTypedUnreachableError(t *testing.T) {
	dir := t.TempDir()
	executable := filepath.Join(dir, "fake-ansible-playbook")
	body := "#!/bin/sh\n" +
		"printf '%s\\n' '{\"host\":\"step_0\",\"status\":\"unreachable\"}' '{\"schemaVersion\":1,\"status\":\"terminal\",\"processedHosts\":[\"step_0\"],\"hosts\":{\"step_0\":{\"ok\":0,\"failed\":0,\"skipped\":0,\"unreachable\":1}}}' > \"$BOOTWRIGHT_ANSIBLE_ARTIFACTS/" + RunResultName + "\"\n" +
		"exit 2\n"
	if err := os.WriteFile(executable, []byte(body), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	err := (CommandRunner{}).Run(context.Background(), RunSpec{
		Executable:          executable,
		Inventory:           "inventory.yaml",
		Playbook:            "playbook.yaml",
		ExtraVars:           "vars.yaml",
		ArtifactsDir:        filepath.Join(dir, "artifacts"),
		ClassifyUnreachable: true,
	})
	if err == nil {
		t.Fatal("Run succeeded")
	}
	if !IsUnreachable(err) {
		t.Fatalf("Run error %T is not an UnreachableError: %v", err, err)
	}
	var unreachable *UnreachableError
	if !errors.As(err, &unreachable) || unreachable.Err == nil {
		t.Fatalf("typed error does not preserve the command failure: %#v", err)
	}
}

func TestCommandRunnerRefusesStaleUnreachableEvidence(t *testing.T) {
	dir := t.TempDir()
	artifactsDir := filepath.Join(dir, "artifacts")
	if err := os.MkdirAll(artifactsDir, 0o700); err != nil {
		t.Fatalf("mkdir artifacts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(artifactsDir, RunResultName), []byte(`{"status":"unreachable"}`+"\n"), 0o600); err != nil {
		t.Fatalf("write stale result: %v", err)
	}
	executable := filepath.Join(dir, "fake-ansible-playbook")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 2\n"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	err := (CommandRunner{}).Run(context.Background(), RunSpec{
		Executable:          executable,
		Inventory:           "inventory.yaml",
		Playbook:            "playbook.yaml",
		ExtraVars:           "vars.yaml",
		ArtifactsDir:        artifactsDir,
		ClassifyUnreachable: true,
	})
	if err == nil {
		t.Fatal("Run succeeded")
	}
	if IsUnreachable(err) {
		t.Fatalf("stale result incorrectly classified a new failure as unreachable: %v", err)
	}
}
