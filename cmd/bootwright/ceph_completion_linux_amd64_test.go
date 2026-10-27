//go:build linux && amd64

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

var fullCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)
var ownerAccepted = regexp.MustCompile(`\baccepted\b`)
var b484Item = regexp.MustCompile(`\bB484\b`)
var cleanLedgerBuild = regexp.MustCompile(`^Commit ([0-9a-f]{40}); Source clean$`)
var cephBaseline = regexp.MustCompile("\\*\\*Acceptance baseline:\\*\\* `([0-9a-f]{40})`")

type cephCompletionEvidence struct {
	state, baseline                string
	profiles, combinations, ledger []map[string]string
	runnable                       func(string) bool
	matchesBaseline                func(string, string) bool
}

// Completion is deliberately stricter than incremental fixture tests: no
// planned profile or observation row can disappear through a filtered list.
func cephCompletionViolations(e cephCompletionEvidence) []string {
	if e.state == "in progress" {
		return nil
	}
	if e.state != "completed" {
		return []string{"M2c must declare in progress or completed"}
	}
	var problems []string
	if !fullCommit.MatchString(e.baseline) {
		problems = append(problems, "B484 needs a registered full acceptance baseline")
	}
	for _, row := range e.combinations {
		if row["Status"] != "tested" {
			problems = append(problems, "unsupported-combination coverage is not tested: "+row["ID"])
		}
	}
	for n := 1; n <= 7; n++ {
		id := fmt.Sprintf("P%d", n)
		var selected []map[string]string
		for _, row := range e.profiles {
			if catalogProfileID(row) == id {
				selected = append(selected, row)
			}
		}
		if len(selected) != 1 {
			problems = append(problems, id+" must appear exactly once")
			continue
		}
		match := cephExampleLinkRegex.FindStringSubmatch(selected[0]["Example"])
		if match == nil || match[1] != match[2] {
			problems = append(problems, id+" needs a linked runnable example")
			continue
		}
		if !e.runnable(match[2]) {
			problems = append(problems, id+" example is missing or not runnable")
		}
		if !cephAcceptedProfile(e, id, match[2]) {
			problems = append(problems, id+" lacks owner-accepted B484 evidence on a matching clean build")
		}
	}
	return problems
}

func cephAcceptedProfile(e cephCompletionEvidence, id, directory string) bool {
	if !fullCommit.MatchString(e.baseline) {
		return false
	}
	for _, row := range e.ledger {
		accepted := row["Accepted by"]
		if !strings.HasPrefix(accepted, "Owner:") || !b484Item.MatchString(accepted) || !ownerAccepted.MatchString(accepted) || strings.Contains(strings.ToLower(accepted), "not ") {
			continue
		}
		if !strings.Contains(row["Example"], "examples/ceph-clusters/"+directory+" ") || !strings.Contains(row["Example"], "("+id+")") {
			continue
		}
		if !strings.Contains(row["Outcome"], id+" matrix qualification:") || strings.Contains(strings.ToLower(row["Outcome"]), "unrun") || strings.Contains(strings.ToLower(row["Outcome"]), "blocked") {
			continue
		}
		build := cleanLedgerBuild.FindStringSubmatch(row["Build"])
		if build != nil && e.matchesBaseline(e.baseline, build[1]) {
			return true
		}
	}
	return false
}

func registeredCephBaselineMatches(repository, baseline, build string) bool {
	command := exec.Command("git", "merge-base", "--is-ancestor", baseline, build)
	command.Dir = repository
	if command.Run() != nil {
		return false
	}
	command = exec.Command("git", "show", build+":specs/milestones/m2.md")
	command.Dir = repository
	data, err := command.Output()
	if err != nil {
		return false
	}
	_, section, found := strings.Cut(string(data), "\n### B484\n")
	if !found {
		return false
	}
	section, _, _ = strings.Cut(section, "\n### ")
	match := cephBaseline.FindStringSubmatch(section)
	return match != nil && match[1] == baseline
}

func TestCephProfileCompletionRequiresAcceptedMatrix(t *testing.T) {
	milestone := "../../specs/milestones/m2.md"
	section := markdownSection(t, milestone, "### M2c", 3)
	state := regexp.MustCompile(`\*\*State: ([a-z ]+)\.\*\*`).FindStringSubmatch(section)
	if state == nil {
		t.Fatal("M2c must declare its completion state")
	}
	baseline := ""
	if match := cephBaseline.FindStringSubmatch(markdownSection(t, milestone, "### B484", 3)); match != nil {
		baseline = match[1]
	}
	evidence := cephCompletionEvidence{state: state[1], baseline: baseline, profiles: catalogRows(t), combinations: combinationRows(t),
		ledger:          markdownTable(t, cephAcceptanceDoc, "## Ledger", []string{"Date", "Build", "Host OS", "Example", "Commands", "Outcome", "Log digest", "Accepted by"}),
		matchesBaseline: func(baseline, build string) bool { return registeredCephBaselineMatches("../..", baseline, build) },
		runnable: func(directory string) bool {
			if _, exists := exampleDirectories(t)[directory]; !exists {
				return false
			}
			state, _ := compileAcceptance(t, exampleDirectory(t, directory))
			if len(productionUnsupported(t, state)) != 0 {
				return false
			}
			definitions := productionDefinitions(t, state, directory)
			_, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
			return len(definitions) > 0 && err == nil
		},
	}
	if problems := cephCompletionViolations(evidence); len(problems) != 0 {
		t.Fatal(problems)
	}
}

func completeCephEvidence() cephCompletionEvidence {
	baseline := strings.Repeat("a", 40)
	e := cephCompletionEvidence{state: "completed", baseline: baseline,
		combinations: []map[string]string{{"ID": "U01", "Status": "tested"}},
		runnable:     func(string) bool { return true }, matchesBaseline: func(baseline, build string) bool { return baseline == build },
	}
	for n := 1; n <= 7; n++ {
		id := fmt.Sprintf("P%d", n)
		directory := "profile-" + id
		e.profiles = append(e.profiles, map[string]string{"Profile": id, "Example": fmt.Sprintf("[%s/](../examples/ceph-clusters/%s/)", directory, directory)})
		e.ledger = append(e.ledger, map[string]string{"Build": "Commit " + baseline + "; Source clean", "Example": "examples/ceph-clusters/" + directory + " (" + id + ") with the lab conventions only", "Outcome": id + " matrix qualification: all required journeys", "Accepted by": "Owner: B484, accepted"})
	}
	return e
}

func TestCephProfileCompletionRejectsIncompleteEvidence(t *testing.T) {
	if problems := cephCompletionViolations(completeCephEvidence()); len(problems) != 0 {
		t.Fatal(problems)
	}
	cases := map[string]func(*cephCompletionEvidence){
		"missing profile":      func(e *cephCompletionEvidence) { e.profiles = e.profiles[1:] },
		"planned profile":      func(e *cephCompletionEvidence) { e.profiles[0]["Example"] = "planned: profile-P1/" },
		"missing directory":    func(e *cephCompletionEvidence) { e.runnable = func(string) bool { return false } },
		"coverage gap":         func(e *cephCompletionEvidence) { e.combinations[0]["Status"] = "gap" },
		"baseline placeholder": func(e *cephCompletionEvidence) { e.baseline = "the commit closing X61" },
		"short baseline":       func(e *cephCompletionEvidence) { e.baseline = e.baseline[:12] },
		"unaccepted row":       func(e *cephCompletionEvidence) { e.ledger[0]["Accepted by"] = "observation, not acceptance" },
		"owner declined":       func(e *cephCompletionEvidence) { e.ledger[0]["Accepted by"] = "Owner: B484, not accepted" },
		"unrun qualification":  func(e *cephCompletionEvidence) { e.ledger[0]["Outcome"] = "P1 matrix qualification: unrun" },
		"wrong item":           func(e *cephCompletionEvidence) { e.ledger[0]["Accepted by"] = "Owner: B476, accepted" },
		"wrong profile":        func(e *cephCompletionEvidence) { e.ledger[0]["Example"] = "examples/ceph-clusters/another (P1)" },
		"dirty build":          func(e *cephCompletionEvidence) { e.ledger[0]["Build"] = "Commit " + e.baseline + "; Source dirty" },
		"non-descendant build": func(e *cephCompletionEvidence) {
			e.ledger[0]["Build"] = "Commit " + strings.Repeat("b", 40) + "; Source clean"
		},
		"unregistered baseline": func(e *cephCompletionEvidence) { e.matchesBaseline = func(string, string) bool { return false } },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := completeCephEvidence()
			mutate(&e)
			if len(cephCompletionViolations(e)) == 0 {
				t.Fatal("completion accepted incomplete evidence")
			}
		})
	}
	e := completeCephEvidence()
	e.state = "in progress"
	e.baseline = ""
	e.ledger = nil
	e.profiles = nil
	if problems := cephCompletionViolations(e); len(problems) != 0 {
		t.Fatalf("active milestone prematurely requires owner evidence: %v", problems)
	}
}

func TestCephProfileCompletionRequiresRegistrationBeforeTheRun(t *testing.T) {
	repository := t.TempDir()
	git := func(arguments ...string) string {
		t.Helper()
		command := exec.Command("git", arguments...)
		command.Dir = repository
		command.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Qualification Fixture", "GIT_AUTHOR_EMAIL=qualification@example.test", "GIT_COMMITTER_NAME=Qualification Fixture", "GIT_COMMITTER_EMAIL=qualification@example.test")
		data, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s: %v", arguments, data, err)
		}
		return strings.TrimSpace(string(data))
	}
	git("init", "--quiet", "--object-format=sha1")
	git("-c", "commit.gpgsign=false", "commit", "--quiet", "--allow-empty", "-m", "qualification baseline")
	baseline := git("rev-parse", "HEAD")
	if registeredCephBaselineMatches(repository, baseline, baseline) {
		t.Fatal("a baseline without registration qualified its own build")
	}
	if err := os.MkdirAll(filepath.Join(repository, "specs", "milestones"), 0700); err != nil {
		t.Fatal(err)
	}
	document := "\n### B484\n\n**Acceptance baseline:** `" + baseline + "`.\n"
	if err := os.WriteFile(filepath.Join(repository, "specs", "milestones", "m2.md"), []byte(document), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "specs/milestones/m2.md")
	git("-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "register qualification baseline")
	registered := git("rev-parse", "HEAD")
	git("-c", "commit.gpgsign=false", "commit", "--quiet", "--allow-empty", "-m", "qualified build")
	build := git("rev-parse", "HEAD")
	if !registeredCephBaselineMatches(repository, baseline, build) {
		t.Fatal("registered clean descendant was refused")
	}
	if registeredCephBaselineMatches(repository, baseline, baseline) {
		t.Fatal("later registration retroactively qualified the old build")
	}
	if registeredCephBaselineMatches(repository, build, registered) {
		t.Fatal("a build preceding the baseline was accepted")
	}
}
