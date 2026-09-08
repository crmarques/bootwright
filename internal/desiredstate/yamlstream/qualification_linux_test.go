//go:build linux && !race

package yamlstream_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/yamlstream"
)

func TestParserQualification(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"wide-mapping", "deep-representation", "parser-depth-ceiling", "comments", "empty-documents", "cross-document-anchors", "aggregate-node-boundary"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, executable, "-test.run=^TestParserQualificationChild$", "-test.timeout=120s", "-test.v")
			command.Env = []string{"BOOTWRIGHT_YAML_QUALIFICATION=" + name}
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("qualification failed: %v\n%s", err, output)
			}
			usage, ok := command.ProcessState.SysUsage().(*syscall.Rusage)
			if !ok {
				t.Fatal("Linux process memory measurement unavailable")
			}
			peak := usage.Maxrss * 1024
			if peak > 1<<30 {
				t.Fatalf("peak RSS %d exceeds the 1 GiB qualification ceiling", peak)
			}
			t.Logf("peakRSS=%d userCPU=%s systemCPU=%s\n%s", peak, command.ProcessState.UserTime(), command.ProcessState.SystemTime(), output)
		})
	}
}

func TestParserQualificationChild(t *testing.T) {
	name := os.Getenv("BOOTWRIGHT_YAML_QUALIFICATION")
	if name == "" {
		t.Skip("qualification subprocess helper")
	}
	var data, wantCode string
	wantDocuments := 0
	switch name {
	case "wide-mapping":
		data = "{" + strings.Repeat("a,", (desiredstate.MaxFileBytes-2)/2) + "}"
		wantCode = "input.limit"
	case "deep-representation":
		data = strings.Repeat("[", 9999) + "x" + strings.Repeat("]", 9999)
		wantCode = "input.limit"
	case "parser-depth-ceiling":
		data = strings.Repeat("[", 10001) + "x" + strings.Repeat("]", 10001)
		wantCode = "yaml.syntax"
	case "comments":
		data = "#!" + strings.Repeat("x", desiredstate.MaxFileBytes-2)
	case "empty-documents":
		data = strings.Repeat("---\n", desiredstate.MaxFileDocuments+1)
		wantCode = "input.limit"
	case "cross-document-anchors":
		var stream strings.Builder
		for i := range 10 {
			fmt.Fprintf(&stream, "---\n&a%d {values: [%s]", i, strings.Repeat("x,", 99000))
			if i > 0 {
				fmt.Fprintf(&stream, ", previous: *a%d", i-1)
			}
			stream.WriteString("}\n")
		}
		data = stream.String()
		wantDocuments = 10
	case "aggregate-node-boundary":
		document := "---\n[" + strings.Repeat("x,", desiredstate.MaxDocumentNodes-2) + "]\n"
		data = strings.Repeat(document, desiredstate.MaxNodes/desiredstate.MaxDocumentNodes)
		wantDocuments = 10
	default:
		t.Fatalf("unknown qualification case %q", name)
	}
	files := sources("input.yaml", data)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	documents, diagnostics, err := (yamlstream.Parser{}).Parse(context.Background(), files)
	runtime.ReadMemStats(&after)
	if wantCode == "" {
		if err != nil || len(diagnostics) != 0 || len(documents) != wantDocuments {
			t.Fatalf("docs=%d diagnostics=%+v err=%v", len(documents), diagnostics, err)
		}
	} else if len(diagnostics) != 1 || diagnostics[0].Code != wantCode || len(documents) != 0 {
		t.Fatalf("expected %s: docs=%d diagnostics=%+v err=%v", wantCode, len(documents), diagnostics, err)
	}
	if wantCode == "input.limit" && err == nil {
		t.Fatal("representation limit did not return terminal failure")
	}
	if wantCode == "yaml.syntax" && err != nil {
		t.Fatalf("parser failure unexpectedly stopped all streams: %v", err)
	}
	t.Logf("inputBytes=%d allocatedBytes=%d allocations=%d heapAfter=%d documents=%d", len(data), after.TotalAlloc-before.TotalAlloc, after.Mallocs-before.Mallocs, after.HeapAlloc, len(documents))
	runtime.KeepAlive(documents)
	runtime.KeepAlive(files)
}
