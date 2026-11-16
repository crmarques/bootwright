package main

import (
	"os"
	"slices"
	"strings"
	"testing"
)

const root = "/repo"

func stream(lines ...string) []byte {
	return []byte(strings.Join(lines, "\n"))
}

func entry(code, file, message string) string {
	return `{"code":"` + code + `","severity":"error","location":{"file":"` + root + "/" + file + `","line":3,"column":2},"message":"` + message + `"}`
}

func TestAListedFindingPasses(t *testing.T) {
	findings := stream(
		entry("SA1019", "a/a.go", "x.Y has been deprecated since Go 1.21: use z"),
		entry("SA1019", "a/a.go", "x.Y has been deprecated since Go 1.21: use z"),
		entry("ST1005", "b/b.go", "error strings should not be capitalized"),
	)
	list := []byte("# comment\nSA1019\ta/a.go\t2\tx.Y has been deprecated\nST1005\tb/b.go\t1\terror strings should not be capitalized\n")
	problems, err := check(findings, list, root)
	if err != nil || len(problems) != 0 {
		t.Fatalf("problems = %q, %v", problems, err)
	}
}

func TestEveryDriftFails(t *testing.T) {
	findings := stream(
		entry("U1000", "c/c.go", "func unused is unused"),
		entry("SA1019", "a/a.go", "x.Y has been deprecated"),
		entry("SA4006", "d/d.go", "this value of v is never used"),
		entry("SA4006", "d/d.go", "this value of v is never used"),
	)
	list := []byte("SA1019\ta/a.go\t2\tx.Y has been deprecated\nSA4006\td/d.go\t1\tthis value of v is never used\nST1005\tb/b.go\t1\terror strings should not be capitalized\n")
	problems, err := check(findings, list, root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"c/c.go:3:2: func unused is unused (U1000) is no listed exclusion",
		"exclusion line 1 covers 1 findings, fewer than it counts; lower it to 1: SA1019\ta/a.go\t2\tx.Y has been deprecated",
		"exclusion line 2 covers 2 findings, more than it counts; fix the new findings: SA4006\td/d.go\t1\tthis value of v is never used",
		"exclusion line 3 matches no finding; remove it: ST1005\tb/b.go\t1\terror strings should not be capitalized",
	}
	if !slices.Equal(problems, want) {
		t.Fatalf("problems = %q, want %q", problems, want)
	}
}

func TestTheLongestMessageStartTakesTheFinding(t *testing.T) {
	findings := stream(entry("SA1019", "a/a.go", "x.Y has been deprecated"), entry("SA1019", "a/a.go", "x.YZ has been deprecated"))
	list := []byte("SA1019\ta/a.go\t1\tx.Y\nSA1019\ta/a.go\t1\tx.YZ\n")
	if problems, err := check(findings, list, root); err != nil || len(problems) != 0 {
		t.Fatalf("problems = %q, %v", problems, err)
	}
}

func TestAMalformedOrUnsortedListFails(t *testing.T) {
	for name, list := range map[string]string{
		"fields":   "SA1019\ta/a.go\tx.Y\n",
		"count":    "SA1019\ta/a.go\t0\tx.Y\n",
		"unsorted": "SA1019\tb/b.go\t1\tx.Y\nSA1019\ta/a.go\t1\tx.Y\n",
		"repeated": "SA1019\ta/a.go\t1\tx.Y\nSA1019\ta/a.go\t1\tx.Y\n",
	} {
		_, problems := parseExclusions([]byte(list))
		if len(problems) == 0 {
			t.Errorf("%s: a %s list passed", name, name)
		}
	}
}

func TestAStreamThatIsNoStaticcheckOutputFails(t *testing.T) {
	for _, findings := range []string{"not json", `{"severity":"error"}`} {
		if _, err := check([]byte(findings), nil, root); err == nil {
			t.Errorf("%q passed", findings)
		}
	}
}

func TestTheTrackedListIsWellFormed(t *testing.T) {
	data, err := os.ReadFile("exclusions.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, problems := parseExclusions(data); len(problems) != 0 {
		t.Fatal(strings.Join(problems, "\n"))
	}
}
