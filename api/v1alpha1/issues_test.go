package v1alpha1

import "testing"

func TestIssueCapIsHeldOnce(t *testing.T) {
	if MaxIssues != 999 {
		t.Fatalf("MaxIssues = %d; specs/api/input.md allows 999 ordinary diagnostics", MaxIssues)
	}
	full := make([]Issue, MaxIssues-1)
	if got := AppendIssues(full, Issue{Code: "a"}, Issue{Code: "b"}); len(got) != MaxIssues || got[MaxIssues-1].Code != "a" {
		t.Fatalf("append past the cap kept %d issues", len(got))
	}
	if got := AppendIssues(make([]Issue, MaxIssues), Issue{Code: "a"}); len(got) != MaxIssues {
		t.Fatalf("append to a full list grew it to %d", len(got))
	}
	if got := AppendIssues(make([]Issue, MaxIssues+5), Issue{Code: "a"}); len(got) != MaxIssues+5 {
		t.Fatalf("append to an overfull list changed it to %d", len(got))
	}
	if got := BoundIssues(make([]Issue, MaxIssues+7)); len(got) != MaxIssues {
		t.Fatalf("bound kept %d issues", len(got))
	}
	if got := ReferenceIssue("f", "m", "r"); got.Code != "api.reference" || got.Field != "f" || got.Message != "m" || got.Remediation != "r" {
		t.Fatalf("reference issue = %+v", got)
	}
}
