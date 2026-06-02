package bastion

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type fakeProcessDeps struct {
	paths       map[string]bool
	pathResults map[string]string
	outputs     map[string][]byte
	uid         int
}

func (f fakeProcessDeps) deps() ProcessDeps {
	return ProcessDeps{
		LookPath: func(name string) (string, error) {
			if f.paths[name] {
				if path := f.pathResults[name]; path != "" {
					return path, nil
				}
				return name, nil
			}
			return "", errors.New("not found")
		},
		CommandOutput: func(name string, args ...string) ([]byte, error) {
			key := commandOutputKey(name, args...)
			if out, ok := f.outputs[key]; ok {
				return out, nil
			}
			if out, ok := f.outputs[name]; ok {
				return out, nil
			}
			return nil, errors.New("command failed")
		},
		UID: func() int { return f.uid },
	}
}

func commandOutputKey(name string, args ...string) string {
	return strings.Join(append([]string{name}, args...), "\x00")
}

func TestParsePythonVersion(t *testing.T) {
	cases := []struct {
		in                   string
		wantMajor, wantMinor int
		wantErr              bool
		wantErrContainsAnyOf []string
	}{
		{in: "Python 3.12.4", wantMajor: 3, wantMinor: 12},
		{in: "3.13.1", wantMajor: 3, wantMinor: 13},
		{in: "Python 4.0.0", wantMajor: 4, wantMinor: 0},
		{in: "Python", wantErr: true, wantErrContainsAnyOf: []string{"unexpected version"}},
		{in: "garbage", wantErr: true, wantErrContainsAnyOf: []string{"unexpected version"}},
		{in: "Python x.y", wantErr: true, wantErrContainsAnyOf: []string{"parse major"}},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			major, minor, err := ParsePythonVersion(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got nil")
				}
				for _, want := range tc.wantErrContainsAnyOf {
					if strings.Contains(err.Error(), want) {
						return
					}
				}
				t.Fatalf("error %q did not contain any of %v", err, tc.wantErrContainsAnyOf)
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if major != tc.wantMajor || minor != tc.wantMinor {
				t.Errorf("got %d.%d, want %d.%d", major, minor, tc.wantMajor, tc.wantMinor)
			}
		})
	}
}

func TestSudoPackageInstallCmd(t *testing.T) {
	cases := []struct {
		name             string
		args             []string
		preserveProxyEnv bool
		want             []string
	}{
		{
			name: "no proxy preservation",
			args: []string{"dnf", "install", "-y", "python3.12"},
			want: []string{"sudo", "dnf", "install", "-y", "python3.12"},
		},
		{
			name:             "with proxy preservation",
			args:             []string{"apt-get", "install", "-y", "python3.12"},
			preserveProxyEnv: true,
			want: []string{
				"sudo",
				"--preserve-env=" + SudoPreservedProxyVars,
				"apt-get", "install", "-y", "python3.12",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SudoPackageInstallCmd(tc.args, tc.preserveProxyEnv)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestStateOpenShiftReleaseVersion(t *testing.T) {
	cases := []struct {
		name     string
		clusters []v1alpha1.ContainerCluster
		want     string
	}{
		{"empty state", nil, ""},
		{
			"cluster without release",
			[]v1alpha1.ContainerCluster{{Metadata: v1alpha1.Metadata{Name: "cluster"}}},
			"",
		},
		{
			"openshift cluster with release",
			[]v1alpha1.ContainerCluster{{
				Metadata: v1alpha1.Metadata{Name: "cluster"},
				Spec:     v1alpha1.ContainerClusterSpec{Distribution: v1alpha1.DistributionSpec{Type: v1alpha1.DistributionOpenShift, Release: v1alpha1.ReleaseSpec{Version: "4.21.12"}}},
			}},
			"4.21.12",
		},
		{
			"release with whitespace",
			[]v1alpha1.ContainerCluster{{
				Spec: v1alpha1.ContainerClusterSpec{Distribution: v1alpha1.DistributionSpec{Type: v1alpha1.DistributionOpenShift, Release: v1alpha1.ReleaseSpec{Version: "  4.21.12  "}}},
			}},
			"4.21.12",
		},
		{
			"okd release ignored",
			[]v1alpha1.ContainerCluster{{
				Spec: v1alpha1.ContainerClusterSpec{Distribution: v1alpha1.DistributionSpec{Type: v1alpha1.DistributionOKD, Release: v1alpha1.ReleaseSpec{Version: "4.21.0-okd"}}},
			}},
			"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := StateOpenShiftReleaseVersion(v1alpha1.State{ContainerClusters: tc.clusters})
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCLIInstallSpecPlannedCommand(t *testing.T) {
	spec := CLIInstallSpec{
		OCPReleaseVersion: "4.21.12",
		InstallDir:        "/usr/local/bin",
		BundleDir:         "/var/lib/bootwright/cache/ansible-bundles/version=dev",
		Executable:        "/venv/bin/ansible-playbook",
	}
	got := spec.PlannedCommand("inv.ini")
	want := []string{
		"/venv/bin/ansible-playbook",
		"-i", "/var/lib/bootwright/cache/ansible-bundles/version=dev/inv.ini",
		"bootwright.core.workflow_bastion_apply_tools",
		"-e", "bootwright_openshift_release_version=4.21.12",
		"-e", "bootwright_clis_install_dir=/usr/local/bin",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("PlannedCommand mismatch\n got %v\nwant %v", got, want)
	}
}

func TestComponentPinnedVersion(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{name: "ansible-core", want: "2.21.0"},
		{name: "pip", want: "26.1.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ComponentPinnedVersion(tc.name)
			if err != nil {
				t.Fatalf("ComponentPinnedVersion(%q): %v", tc.name, err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	if _, err := ComponentPinnedVersion("missing-tool"); err == nil {
		t.Fatal("expected error for missing component pin")
	}
}

func TestPlanCLIInstallSkipsWhenNoVersion(t *testing.T) {
	got := PlanCLIInstall(v1alpha1.State{}, "/usr/local/bin", "/bundle", func(name string) string { return "/venv/bin/" + name })
	if got != nil {
		t.Errorf("expected nil when no release version, got %+v", got)
	}
}

func TestPlanCLIInstallPopulatesWhenVersionPresent(t *testing.T) {
	state := v1alpha1.State{
		ContainerClusters: []v1alpha1.ContainerCluster{{
			Spec: v1alpha1.ContainerClusterSpec{Distribution: v1alpha1.DistributionSpec{Type: v1alpha1.DistributionOpenShift, Release: v1alpha1.ReleaseSpec{Version: "4.21.12"}}},
		}},
	}
	got := PlanCLIInstall(state, "/usr/local/bin", "/bundle", func(name string) string { return "/venv/bin/" + name })
	if got == nil {
		t.Fatal("expected non-nil spec")
	}
	if got.OCPReleaseVersion != "4.21.12" {
		t.Errorf("OCPReleaseVersion = %q, want %q", got.OCPReleaseVersion, "4.21.12")
	}
	if got.Executable != "/venv/bin/ansible-playbook" {
		t.Errorf("Executable = %q, want %q", got.Executable, "/venv/bin/ansible-playbook")
	}
}

func TestResolvePython312WithDeps(t *testing.T) {
	got, ok := ResolvePython312With(fakeProcessDeps{
		paths:   map[string]bool{"python3": true},
		outputs: map[string][]byte{"python3": []byte("Python 3.13.1")},
	}.deps())
	if !ok || got != "python3" {
		t.Fatalf("ResolvePython312With = %q, %v; want python3, true", got, ok)
	}
	if got, ok := ResolvePython312With(fakeProcessDeps{
		paths:   map[string]bool{"python3": true},
		outputs: map[string][]byte{"python3": []byte("Python 3.11.9")},
	}.deps()); ok || got != "" {
		t.Fatalf("old Python accepted: %q, %v", got, ok)
	}
	got, ok = ResolvePython312With(fakeProcessDeps{
		paths: map[string]bool{
			"python3":    true,
			"python3.13": true,
		},
		outputs: map[string][]byte{
			"python3":    []byte("Python 3.11.9"),
			"python3.13": []byte("Python 3.13.1"),
		},
	}.deps())
	if !ok || got != "python3.13" {
		t.Fatalf("ResolvePython312With = %q, %v; want python3.13, true", got, ok)
	}
}

func TestResolvePython312WithDepsUsesLookPathResult(t *testing.T) {
	python := "/home/operator/.local/bin/python3.12"
	got, ok := ResolvePython312With(fakeProcessDeps{
		paths:       map[string]bool{"python3.12": true},
		pathResults: map[string]string{"python3.12": python},
		outputs:     map[string][]byte{python: []byte("Python 3.12.4")},
	}.deps())
	if !ok || got != python {
		t.Fatalf("ResolvePython312With = %q, %v; want %s, true", got, ok, python)
	}
}

func TestPython312InstallCmdWithDeps(t *testing.T) {
	cases := []struct {
		name             string
		deps             fakeProcessDeps
		preserveProxyEnv bool
		want             []string
	}{
		{
			name: "root uses dnf directly",
			deps: fakeProcessDeps{
				paths: map[string]bool{"dnf": true},
				uid:   0,
			},
			want: []string{"dnf", "install", "-y", "python3.12"},
		},
		{
			name: "non-root uses sudo with proxy preservation",
			deps: fakeProcessDeps{
				paths: map[string]bool{"apt-get": true, "sudo": true},
				uid:   1000,
			},
			preserveProxyEnv: true,
			want: []string{
				"sudo",
				"--preserve-env=" + SudoPreservedProxyVars,
				"apt-get", "install", "-y", "python3.12",
			},
		},
		{
			name: "no supported package manager",
			deps: fakeProcessDeps{
				paths: map[string]bool{},
				uid:   1000,
			},
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Python312InstallCmdWith(tc.deps.deps(), tc.preserveProxyEnv)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBootstrapPlanWithDepsAddsPythonInstallWhenMissing(t *testing.T) {
	got, err := BootstrapPlanWith(fakeProcessDeps{
		paths: map[string]bool{"dnf": true},
		uid:   0,
	}.deps(), "/venv", func(name string) string { return "/venv/bin/" + name }, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d steps, want 4: %+v", len(got), got)
	}
	if !reflect.DeepEqual(got[0].Cmd, []string{"dnf", "install", "-y", "python3.12"}) {
		t.Fatalf("first step = %v", got[0].Cmd)
	}
}

func TestBootstrapPlanWithDepsUsesExistingPython(t *testing.T) {
	got, err := BootstrapPlanWith(fakeProcessDeps{
		paths:   map[string]bool{"python3.12": true},
		outputs: map[string][]byte{"python3.12": []byte("Python 3.12.4")},
		uid:     1000,
	}.deps(), "/venv", func(name string) string { return "/venv/bin/" + name }, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d steps, want 3: %+v", len(got), got)
	}
	if got[0].Cmd[0] != "python3.12" {
		t.Fatalf("first step command = %v", got[0].Cmd)
	}
}

func TestBootstrapPlanWithDepsSkipsPinnedVenv(t *testing.T) {
	venvBin := func(name string) string { return "/venv/bin/" + name }
	got, err := BootstrapPlanWith(fakeProcessDeps{
		paths: map[string]bool{"python3.12": true},
		outputs: map[string][]byte{
			"python3.12":      []byte("Python 3.12.4"),
			venvBin("python"): []byte("Python 3.12.4"),
			commandOutputKey(venvBin("python"), "-m", "pip", "--version"):            []byte("pip 26.1.1 from /venv/lib/python3.12/site-packages/pip (python 3.12)"),
			commandOutputKey(venvBin("python"), "-m", "pip", "show", "ansible-core"): []byte("Name: ansible-core\nVersion: 2.21.0\n"),
		},
	}.deps(), "/venv", venvBin, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d steps, want 0: %+v", len(got), got)
	}
}

func TestBootstrapPlanWithDepsSkipsPinnedVenvWithoutSystemPython(t *testing.T) {
	venvBin := func(name string) string { return "/venv/bin/" + name }
	got, err := BootstrapPlanWith(fakeProcessDeps{
		outputs: map[string][]byte{
			venvBin("python"): []byte("Python 3.12.4"),
			commandOutputKey(venvBin("python"), "-m", "pip", "--version"):            []byte("pip 26.1.1 from /venv/lib/python3.12/site-packages/pip (python 3.12)"),
			commandOutputKey(venvBin("python"), "-m", "pip", "show", "ansible-core"): []byte("Name: ansible-core\nVersion: 2.21.0\n"),
		},
	}.deps(), "/venv", venvBin, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d steps, want 0: %+v", len(got), got)
	}
}

func TestBootstrapPlanWithDepsRecreatesOutdatedPinnedVenv(t *testing.T) {
	venvBin := func(name string) string { return "/venv/bin/" + name }
	got, err := BootstrapPlanWith(fakeProcessDeps{
		paths: map[string]bool{"python3.12": true},
		outputs: map[string][]byte{
			"python3.12":      []byte("Python 3.12.4"),
			venvBin("python"): []byte("Python 3.12.4"),
			commandOutputKey(venvBin("python"), "-m", "pip", "--version"):            []byte("pip 26.1.1 from /venv/lib/python3.12/site-packages/pip (python 3.12)"),
			commandOutputKey(venvBin("python"), "-m", "pip", "show", "ansible-core"): []byte("Name: ansible-core\nVersion: 2.19.0\n"),
		},
	}.deps(), "/venv", venvBin, false, false)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"python3.12", "-m", "venv", "--clear", "/venv"},
		{"/venv/bin/python", "-m", "pip", "install", "pip==26.1.1"},
		{"/venv/bin/python", "-m", "pip", "install", "ansible-core==2.21.0"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d steps, want %d: %+v", len(got), len(want), got)
	}
	for i, step := range got {
		if !reflect.DeepEqual(step.Cmd, want[i]) {
			t.Fatalf("step %d = %v, want %v", i, step.Cmd, want[i])
		}
	}
}

func TestBootstrapPlanWithDepsRepairsUsableVenvWithoutSystemPython(t *testing.T) {
	venvBin := func(name string) string { return "/venv/bin/" + name }
	got, err := BootstrapPlanWith(fakeProcessDeps{
		outputs: map[string][]byte{
			venvBin("python"): []byte("Python 3.12.4"),
			commandOutputKey(venvBin("python"), "-m", "pip", "--version"):            []byte("pip 25.0.0 from /venv/lib/python3.12/site-packages/pip (python 3.12)"),
			commandOutputKey(venvBin("python"), "-m", "pip", "show", "ansible-core"): []byte("Name: ansible-core\nVersion: 2.19.0\n"),
		},
	}.deps(), "/venv", venvBin, false, false)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"/venv/bin/python", "-m", "pip", "install", "pip==26.1.1"},
		{"/venv/bin/python", "-m", "pip", "install", "ansible-core==2.21.0"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d steps, want %d: %+v", len(got), len(want), got)
	}
	for i, step := range got {
		if !reflect.DeepEqual(step.Cmd, want[i]) {
			t.Fatalf("step %d = %v, want %v", i, step.Cmd, want[i])
		}
	}
}

func TestBootstrapPlanWithDepsWrapsRootManagedVenvSteps(t *testing.T) {
	got, err := BootstrapPlanWith(fakeProcessDeps{
		paths:   map[string]bool{"python3.12": true, "sudo": true},
		outputs: map[string][]byte{"python3.12": []byte("Python 3.12.4")},
		uid:     1000,
	}.deps(), "/var/lib/bootwright/ansible-venv", func(name string) string {
		return "/var/lib/bootwright/ansible-venv/bin/" + name
	}, true, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d steps, want 3: %+v", len(got), got)
	}
	for _, step := range got {
		if len(step.Cmd) < 3 || step.Cmd[0] != "sudo" || step.Cmd[1] != "--preserve-env="+SudoPreservedProxyVars {
			t.Fatalf("step was not sudo-wrapped with proxy preservation: %+v", step)
		}
		if !strings.Contains(step.Label, "(requires sudo)") {
			t.Fatalf("step label does not mention sudo: %q", step.Label)
		}
	}
}

func TestMergeBootstrapEnvStripsProxyAndAppliesExtra(t *testing.T) {
	base := []string{
		"PATH=/bin:/usr/bin",
		"HTTP_PROXY=http://proxy:8080",
		"https_proxy=http://proxy:8443",
		"USER=test",
	}
	extra := map[string]string{
		"HTTP_PROXY":     "http://other:3128",
		"ANSIBLE_CONFIG": "/bundle/ansible.cfg",
	}
	got := MergeBootstrapEnv(base, extra)

	// PATH and USER from base are preserved.
	if !slices.Contains(got, "PATH=/bin:/usr/bin") {
		t.Errorf("PATH was dropped: %v", got)
	}
	if !slices.Contains(got, "USER=test") {
		t.Errorf("USER was dropped: %v", got)
	}
	// Original proxy entries are stripped (we don't want the base ones
	// leaking through when the operator sets a different proxy).
	for _, kv := range got {
		if strings.HasPrefix(kv, "https_proxy=") && kv != "https_proxy=" {
			// The ORIGINAL value should not survive — the operator
			// decides what proxy applies.
			if strings.Contains(kv, "proxy:8443") {
				t.Errorf("original https_proxy survived in env: %q", kv)
			}
		}
	}
	// Extra overrides land.
	if !slices.Contains(got, "HTTP_PROXY=http://other:3128") {
		t.Errorf("extra HTTP_PROXY missing: %v", got)
	}
	if !slices.Contains(got, "ANSIBLE_CONFIG=/bundle/ansible.cfg") {
		t.Errorf("extra ANSIBLE_CONFIG missing: %v", got)
	}
}

func TestMergeBootstrapEnvWithEmptyExtra(t *testing.T) {
	base := []string{"PATH=/bin", "HTTP_PROXY=http://x:1"}
	got := MergeBootstrapEnv(base, nil)
	// Proxy is stripped even with no extra overrides — that's the
	// "bootstrap" part of the function name: the bootstrap subprocess
	// must not inherit ambient HTTP_PROXY.
	for _, kv := range got {
		if strings.HasPrefix(kv, "HTTP_PROXY=") {
			t.Errorf("HTTP_PROXY should be stripped, got %q", kv)
		}
	}
	if !slices.Contains(got, "PATH=/bin") {
		t.Errorf("PATH dropped: %v", got)
	}
}
