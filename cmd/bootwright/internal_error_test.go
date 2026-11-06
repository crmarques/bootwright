package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
)

type panickingState struct{ cli.DesiredStateService }

func (panickingState) RenderEffective(context.Context, compilation.EffectiveRequest) (*compilation.EffectiveResult, error) {
	panic("PRIVATE-PANIC-MATERIAL")
}

func TestInternalFailureHasOnlyASafeSummaryAndCorrelationIdentity(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		var out, errOut bytes.Buffer
		var escaped any
		code := 0
		func() {
			defer func() { escaped = recover() }()
			code = runServices(context.Background(), []string{"render", "effective", "--output", format}, &out, &errOut, cli.Services{DesiredState: panickingState{}})
		}()
		combined := out.String() + errOut.String()
		if escaped != nil || code != 1 || !strings.Contains(combined, "runtime.internal") || !strings.Contains(combined, "reference ") {
			t.Fatalf("%s: escaped=%t exit=%d output=%q", format, escaped != nil, code, combined)
		}
		for _, prohibited := range []string{"PRIVATE-PANIC-MATERIAL", "goroutine", "panic:", "runtime/debug"} {
			if strings.Contains(combined, prohibited) {
				t.Errorf("%s exposes %s", format, prohibited)
			}
		}
		if format == "json" && (!json.Valid(out.Bytes()) || errOut.Len() != 0) {
			t.Fatalf("structured panic output=%q, stderr=%q", out.String(), errOut.String())
		}
	}
}
