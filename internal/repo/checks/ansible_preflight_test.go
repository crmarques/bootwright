package repocheck

import (
	"fmt"
	"strings"
	"testing"
)

const checkPreflightPlaybookPath = "ansible/collections/ansible_collections/bootwright/core/playbooks/check_preflight.yml"

func preflightHostPrerequisitesPlay(t *testing.T) map[string]any {
	t.Helper()
	for _, play := range readAnsiblePlays(t, checkPreflightPlaybookPath) {
		if play["name"] == "Check host prerequisites" {
			return play
		}
	}
	t.Fatalf("%s has no %q play", checkPreflightPlaybookPath, "Check host prerequisites")
	return nil
}

func taskWhenClauses(t *testing.T, task map[string]any) string {
	t.Helper()
	switch when := task["when"].(type) {
	case string:
		return when
	case []any:
		clauses := make([]string, 0, len(when))
		for _, clause := range when {
			clauses = append(clauses, fmt.Sprintf("%v", clause))
		}
		return strings.Join(clauses, " && ")
	default:
		t.Fatalf("task %v has no when clause", task["name"])
		return ""
	}
}

func TestPreflightProbesTheHostConnectionBeforeItChecksTheHost(t *testing.T) {
	play := preflightHostPrerequisitesPlay(t)
	if play["gather_facts"] != false {
		t.Fatalf("%s play %q must gather facts through its own probe task so an unreachable host is classified, got gather_facts=%v",
			checkPreflightPlaybookPath, play["name"], play["gather_facts"])
	}
	if _, ok := play["ignore_unreachable"]; ok {
		t.Fatalf("%s play %q must not ignore unreachable play-wide; a host that drops after the probe has to fail the run",
			checkPreflightPlaybookPath, play["name"])
	}
	tasks := nestedAnsibleTasks(t, play, "pre_tasks")
	probe := tasks[findAnsibleTask(t, tasks, "Open the host connection")]
	if probe["ignore_unreachable"] != true {
		t.Fatalf("the connection probe must set ignore_unreachable so preflight classifies the host instead of aborting")
	}
	if probe["register"] != "bootwright_preflight_connection" {
		t.Fatalf("the connection probe must register bootwright_preflight_connection, got %v", probe["register"])
	}
	probeIndex := findAnsibleTask(t, tasks, "Open the host connection")
	for _, name := range []string{
		"Report a host this run installs before anything logs in to it",
		"Refuse an unreachable host this run does not install",
		"Stop checking a host that is not reachable",
	} {
		if findAnsibleTask(t, tasks, name) <= probeIndex {
			t.Fatalf("task %q must run after the connection probe it reads", name)
		}
	}
}

func TestPreflightDefersAHostTheRunInstallsAndRefusesOneItDoesNot(t *testing.T) {
	play := preflightHostPrerequisitesPlay(t)
	tasks := nestedAnsibleTasks(t, play, "pre_tasks")

	report := tasks[findAnsibleTask(t, tasks, "Report a host this run installs before anything logs in to it")]
	reportWhen := taskWhenClauses(t, report)
	if !strings.Contains(reportWhen, "bootwright_preflight_connection.unreachable") {
		t.Fatalf("the deferral report must be gated on the probe result, got when=%q", reportWhen)
	}
	if !strings.Contains(reportWhen, "not (bootwright_os_provided | default(true) | bool)") {
		t.Fatalf("only a machine whose OS this run installs may be deferred, got when=%q", reportWhen)
	}

	refuse := tasks[findAnsibleTask(t, tasks, "Refuse an unreachable host this run does not install")]
	if _, ok := refuse["ansible.builtin.fail"]; !ok {
		t.Fatalf("an unreachable host the run does not install must fail preflight, got %v", refuse)
	}
	refuseWhen := taskWhenClauses(t, refuse)
	if !strings.Contains(refuseWhen, "bootwright_preflight_connection.unreachable") {
		t.Fatalf("the refusal must be gated on the probe result, got when=%q", refuseWhen)
	}
	if !strings.Contains(refuseWhen, "bootwright_os_provided | default(true) | bool") {
		t.Fatalf("a host with no declared OS provenance must default to must-be-reachable, got when=%q", refuseWhen)
	}

	end := tasks[findAnsibleTask(t, tasks, "Stop checking a host that is not reachable")]
	if end["ansible.builtin.meta"] != "end_host" {
		t.Fatalf("an unreachable host must leave the play instead of retrying every check, got %v", end)
	}
	if !strings.Contains(taskWhenClauses(t, end), "bootwright_preflight_connection.unreachable") {
		t.Fatalf("the play must end only the hosts the probe could not reach")
	}
}
