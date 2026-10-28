//go:build linux && amd64

package ansiblerunner

import (
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

type completedFixture struct{ fallback error }

func (c completedFixture) Error() string { return c.fallback.Error() }
func (c completedFixture) Unwrap() error { return c.fallback }
func (completedFixture) WithDetail(detail string) error {
	return failure("lifecycle.state", "the machine reports "+detail, "run it again")
}

func TestARefusalTheDetailCompletesTakesTheDetailInItsMessage(t *testing.T) {
	named := completedFixture{fallback: failure("lifecycle.state", "the machine reports unknown", "run it again")}
	if reported := diagnostics.Of(refusalDetail(named, "9.6")); len(reported) != 1 || reported[0].Message != "the machine reports 9.6" ||
		reported[0].Remediation != "run it again" {
		t.Fatalf("completed refusal = %#v", reported)
	}
	if reported := diagnostics.Of(refusalDetail(named, "")); len(reported) != 1 || reported[0].Message != "the machine reports unknown" {
		t.Fatalf("a refusal without detail = %#v", reported)
	}
	plain := failure("lifecycle.state", "the adapter refused", "")
	if reported := diagnostics.Of(refusalDetail(plain, "cause")); len(reported) != 1 || reported[0].Message != "the adapter refused (adapter detail: cause)" {
		t.Fatalf("an appended detail = %#v", reported)
	}
}
