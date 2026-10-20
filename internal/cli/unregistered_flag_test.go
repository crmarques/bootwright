package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestDestroyNamesEachUnregisteredMachine(t *testing.T) {
	code, _, errOut, record := runRecorded([]string{"--context", "lab", "destroy", "--unregistered", "a,b", "--unregistered", "c"})
	request, ok := record.request.(lifecycle.DestroyRequest)
	if code != 1 || record.calls != 1 || !ok || !slices.Equal(request.Unregistered, []string{"a", "b", "c"}) {
		t.Fatalf("code=%d calls=%d request=%#v stderr=%q", code, record.calls, record.request, errOut)
	}
	code, _, _, record = runRecorded([]string{"--context", "lab", "destroy"})
	if request, ok := record.request.(lifecycle.DestroyRequest); code != 1 || !ok || request.Unregistered != nil {
		t.Fatalf("a destroy naming no Machine = %#v", record.request)
	}
}

func TestAnEmptyUnregisteredMemberIsAUsageError(t *testing.T) {
	for _, args := range [][]string{
		{"destroy", "--unregistered", "a,,b"},
		{"destroy", "--unregistered", "a,"},
		{"destroy", "--unregistered", "a", "--unregistered", " "},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, out, errOut, record := runRecorded(args)
			want := "  error  cli.usage\n    --unregistered must not contain an empty member\n"
			if code != 2 || out != "" || record.calls != 0 || !strings.HasPrefix(errOut, want) {
				t.Fatalf("code=%d out=%q calls=%d stderr=%q, want code 2, no call and stderr starting %q", code, out, record.calls, errOut, want)
			}
		})
	}
	code, _, errOut, record := runRecorded([]string{"destroy", "--unregistered", ""})
	if code != 2 || record.calls != 0 || !strings.HasPrefix(errOut, "  error  cli.usage\n    --unregistered must not contain an empty occurrence\n") {
		t.Fatalf("an empty occurrence: code=%d calls=%d stderr=%q", code, record.calls, errOut)
	}
}

func TestOnlyDestroyAcceptsUnregistered(t *testing.T) {
	code, _, errOut, record := runRecorded([]string{"apply", "--unregistered", "a"})
	if code != 2 || record.calls != 0 || !strings.Contains(errOut, "cli.usage") {
		t.Fatalf("apply --unregistered: code=%d calls=%d stderr=%q", code, record.calls, errOut)
	}
}

func TestAnEmptyUnregisteredMemberIsRefusedBeforeElevation(t *testing.T) {
	for _, args := range [][]string{
		{"--context", "lab", "destroy", "--unregistered", "a,"},
		{"--context", "lab", "destroy", "--unregistered", "a,,b"},
		{"--context", "lab", "destroy", "--unregistered", "a", "--unregistered", " "},
	} {
		if class := ClassifyInvocation(args); class.RequiresRoot {
			t.Errorf("%v requires root, want a usage error before elevation", args)
		}
	}
	if class := ClassifyInvocation([]string{"--context", "lab", "destroy", "--unregistered", "a, b"}); !class.RequiresRoot {
		t.Fatal("a destroy naming Machines does not require root")
	}
}
