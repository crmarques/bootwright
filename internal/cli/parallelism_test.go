package cli

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestLifecycleParallelismDispatch(t *testing.T) {
	for _, verb := range []string{"apply", "destroy"} {
		for _, test := range []struct {
			args []string
			want int
		}{
			{nil, 8}, {[]string{"--parallelism", "1"}, 1},
			{[]string{"--parallelism=16"}, 16},
			{[]string{"--parallelism", "2", "--parallelism", "12"}, 12},
		} {
			args := append([]string{verb}, test.args...)
			code, _, stderr, record := runRecorded(args)
			var got int
			switch request := record.request.(type) {
			case lifecycle.ApplyRequest:
				got = request.Parallelism
			case lifecycle.DestroyRequest:
				got = request.Parallelism
			}
			if code != 1 || record.calls != 1 || got != test.want {
				t.Fatalf("%v: code=%d calls=%d request=%#v stderr=%q", args, code, record.calls, record.request, stderr)
			}
		}
	}
}

func TestInvalidParallelismRefusesBeforeDispatchAndElevation(t *testing.T) {
	for _, verb := range []string{"apply", "destroy"} {
		for _, value := range []string{"0", "-1", "", " ", "eight", "1.5", "99999999999999999999999999999999"} {
			args := []string{verb, "--parallelism=" + value}
			code, out, stderr, record := runRecorded(args)
			if code != 2 || out != "" || record.calls != 0 || !strings.Contains(stderr, "--parallelism must") {
				t.Fatalf("%v: code=%d out=%q calls=%d stderr=%q", args, code, out, record.calls, stderr)
			}
			if ClassifyInvocation(args).RequiresRoot {
				t.Fatalf("%v requires elevation", args)
			}
		}
		args := []string{verb, "--parallelism=", "--parallelism=16"}
		if code, _, _, record := runRecorded(args); code != 2 || record.calls != 0 {
			t.Fatalf("an empty occurrence was overridden: code=%d calls=%d", code, record.calls)
		}
	}
}

func TestOnlyLifecycleMutationsAcceptParallelism(t *testing.T) {
	for _, verb := range []string{"plan", "status", "diff"} {
		code, _, _, record := runRecorded([]string{verb, "--parallelism=16"})
		if code != 2 || record.calls != 0 {
			t.Fatalf("%s accepted parallelism: code=%d calls=%d", verb, code, record.calls)
		}
	}
}
