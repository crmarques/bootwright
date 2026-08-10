package repocheck

import (
	"fmt"
	"strings"
	"testing"
)

func TestIBMSubscriptionPreflightScopesOneQueryToDeclaredRepositories(t *testing.T) {
	tasks := readAnsibleTasks(t, "ansible/collections/ansible_collections/bootwright/core/roles/storage_cluster_cephadm/tasks/providers/ibm.yml")
	resolveIdx := findAnsibleTask(t, tasks, "Resolve the declared IBM Ceph package repository probe")
	probeIdx := findAnsibleTask(t, tasks, "Probe only the declared subscription repositories for all IBM Ceph packages")
	readableIdx := findAnsibleTask(t, tasks, "Require every declared IBM Ceph subscription repository to be readable")
	contentIdx := findAnsibleTask(t, tasks, "Require the declared subscription repositories to serve every IBM Ceph package")
	buildsIdx := findAnsibleTask(t, tasks, "Resolve the cephadm builds the declared subscription repositories serve")
	if !(resolveIdx < probeIdx && probeIdx < readableIdx && readableIdx < contentIdx && contentIdx < buildsIdx) {
		t.Fatalf("IBM subscription preflight must resolve, query once, prove readability, attribute every package, then resolve builds: %d %d %d %d %d", resolveIdx, probeIdx, readableIdx, contentIdx, buildsIdx)
	}

	resolve, ok := tasks[resolveIdx]["ansible.builtin.set_fact"].(map[string]any)
	if !ok {
		t.Fatalf("IBM repository probe resolution must be set_fact, got %v", tasks[resolveIdx])
	}
	packages := fmt.Sprint(resolve["bootwright_ceph_subscription_probe_packages"])
	if !strings.Contains(packages, "cephadmPackage") || !strings.Contains(packages, "ibm-storage-ceph-license") {
		t.Fatalf("one probe must carry both required package names, got %s", packages)
	}
	repoArgs := fmt.Sprint(resolve["bootwright_ceph_subscription_probe_repo_args"])
	for _, want := range []string{"repository.redhatRepos", "--enablerepo=", "--setopt=", ".skip_if_unavailable=False"} {
		if !strings.Contains(repoArgs, want) {
			t.Fatalf("declared repository arguments must contain %q, got %s", want, repoArgs)
		}
	}

	probe := tasks[probeIdx]
	command, ok := probe["ansible.builtin.command"].(map[string]any)
	if !ok {
		t.Fatalf("IBM subscription preflight must use command argv, got %v", probe)
	}
	argv := fmt.Sprint(command["argv"])
	for _, want := range []string{"--setopt=skip_if_unavailable=False", "--disablerepo=*", "bootwright_ceph_subscription_probe_repo_args", "repoquery", "build %{name} %{name}", "bootwright_ceph_subscription_probe_packages"} {
		if !strings.Contains(argv, want) {
			t.Fatalf("scoped package probe argv must contain %q, got %s", want, argv)
		}
	}
	if _, exists := probe["loop"]; exists {
		t.Fatalf("IBM package preflight must query all packages in one dnf invocation, got loop=%v", probe["loop"])
	}
	if got := fmt.Sprint(probe["until"]); !strings.Contains(got, "attempts") || !strings.Contains(got, "probe_retries") {
		t.Fatalf("scoped repoquery must keep the bounded retry escape, got %s", got)
	}

	readable := tasks[readableIdx]
	readableBody, ok := readable["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("repository readability gate must be an assert, got %v", readable)
	}
	if got := fmt.Sprint(readableBody["that"]); !strings.Contains(got, ".rc") || strings.Contains(got, "stdout") {
		t.Fatalf("readability gate must classify only command execution, got %s", got)
	}
	readableFailure := fmt.Sprint(readableBody["fail_msg"])
	for _, want := range []string{"undeclared repository was disabled", "cmd", "map('quote')", "bootwright_mutating_invocation"} {
		if !strings.Contains(readableFailure, want) {
			t.Fatalf("readability refusal must retain %q, got %s", want, readableFailure)
		}
	}
	for _, forbidden := range []string{"every enabled repository", "need not be a Ceph one", "re-run apply"} {
		if strings.Contains(readableFailure, forbidden) {
			t.Fatalf("readability refusal retained unscoped or inexact guidance %q: %s", forbidden, readableFailure)
		}
	}

	content := tasks[contentIdx]
	if got := fmt.Sprint(content["loop"]); !strings.Contains(got, "bootwright_ceph_subscription_probe_packages") {
		t.Fatalf("content gate must attribute the one query to every package, got loop=%s", got)
	}
	contentBody, ok := content["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("package content gate must be an assert, got %v", content)
	}
	if got := fmt.Sprint(contentBody["that"]); !strings.Contains(got, "^build ") || !strings.Contains(got, "item | regex_escape") {
		t.Fatalf("package content gate must select build lines by emitted package name, got %s", got)
	}
	if failure := fmt.Sprint(contentBody["fail_msg"]); !strings.Contains(failure, "were read successfully") || !strings.Contains(failure, "bootwright_mutating_invocation") {
		t.Fatalf("package content refusal must distinguish readability and carry the exact retry, got %s", failure)
	}

	builds := fmt.Sprint(tasks[buildsIdx])
	if strings.Contains(builds, ".results") || !strings.Contains(builds, "subscription_preflight.stdout_lines") || !strings.Contains(builds, "^build ") || !strings.Contains(builds, "^spec ") {
		t.Fatalf("cephadm build resolution must consume the one package-attributed stdout, got %s", builds)
	}
	pinned := tasks[findAnsibleTask(t, tasks, "Require the declared subscription repositories to serve the pinned cephadm build")]
	pinnedBody, ok := pinned["ansible.builtin.assert"].(map[string]any)
	if !ok {
		t.Fatalf("pinned build gate must be an assert, got %v", pinned)
	}
	pinnedFailure := fmt.Sprint(pinnedBody["fail_msg"])
	if !strings.Contains(pinnedFailure, "declared repositories") || !strings.Contains(pinnedFailure, "bootwright_mutating_invocation") || strings.Contains(pinnedFailure, "re-run apply") {
		t.Fatalf("pinned build refusal must name only declared repositories and the exact retry, got %s", pinnedFailure)
	}
}
