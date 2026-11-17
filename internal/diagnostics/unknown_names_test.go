package diagnostics

import "testing"

func TestUnknownNamesNamesEveryUnknownAndNothingElse(t *testing.T) {
	if err := UnknownNames("Secret", "lab", []string{"a", "b"}, []string{"b", "a", "c"}); err != nil {
		t.Fatalf("err = %v", err)
	}
	reported := Of(UnknownNames("Secret", "lab", []string{"x", "a", "y"}, []string{"a", "c"}))
	if len(reported) != 1 || reported[0].Code != "access.target" || reported[0].Message != "the context lab selects no Secrets named x, y" ||
		reported[0].Remediation != "name a Secret the context selects; bootwright secret list --context lab lists them" {
		t.Fatalf("diagnostics = %+v", reported)
	}
	reported = Of(UnknownNames("Machine", "lab", []string{"x"}, nil))
	if len(reported) != 1 || reported[0].Message != "the context lab selects no Machine named x" {
		t.Fatalf("diagnostics = %+v", reported)
	}
}
