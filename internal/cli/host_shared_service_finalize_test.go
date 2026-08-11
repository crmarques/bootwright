package cli

import (
	"errors"
	"strings"
	"testing"
)

func TestFinishHostSharedServiceOperationsPreservesPrimaryExitAndAddsFinalizerFailure(t *testing.T) {
	primary := failErr(7, errors.New("primary mutation failed"))
	got := finishHostSharedServiceOperations(primary, func() error {
		return errors.New("remote guard differs")
	})
	var exited *exitError
	if !errors.As(got, &exited) || exited.code != 7 {
		t.Fatalf("result = %#v, want exit code 7", got)
	}
	for _, want := range []string{"primary mutation failed", "additionally failed to finalize", "remote guard differs"} {
		if !strings.Contains(got.Error(), want) {
			t.Fatalf("error = %q, want %q", got, want)
		}
	}
}

func TestFinishHostSharedServiceOperationsRunsOnSuccessAndFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		primary error
	}{
		{name: "success"},
		{name: "failure", primary: errors.New("failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			got := finishHostSharedServiceOperations(tc.primary, func() error {
				calls++
				return nil
			})
			if calls != 1 {
				t.Fatalf("finalizer calls = %d, want 1", calls)
			}
			if !errors.Is(got, tc.primary) {
				t.Fatalf("result = %v, want primary %v", got, tc.primary)
			}
		})
	}
}

func TestFinishHostSharedServiceOperationsSurfacesStandaloneFinalizerFailure(t *testing.T) {
	got := finishHostSharedServiceOperations(nil, func() error {
		return errors.New("release refused")
	})
	var exited *exitError
	if !errors.As(got, &exited) || exited.code != 1 || !strings.Contains(got.Error(), "release refused") {
		t.Fatalf("result = %#v, want exit 1 release refusal", got)
	}
}

func TestHostSharedServiceFinalizationRefusalNamesExactRetry(t *testing.T) {
	invocation := resolvedInvocation{verb: invocationApply, contextName: "matrix", flags: invocationFlags{selection: runSelection{stage: "infra"}, mode: "reconcile", yes: true}}
	got := hostSharedServiceFinalizationRefusal(errors.New("host provider-a unreachable"), invocation)
	for _, want := range []string{"host provider-a unreachable", "guard was retained", "bootwright apply", "--context matrix", "--stage infra"} {
		if !strings.Contains(got.Error(), want) {
			t.Fatalf("error = %q, want %q", got, want)
		}
	}
}
