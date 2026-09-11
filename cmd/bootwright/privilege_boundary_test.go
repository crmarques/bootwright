package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestElevatedStderrWithholdsOnlySudoRefusals(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		withhold bool
		writes   []string
		want     string
		withheld bool
	}{
		{name: "interactive keeps the prompt", writes: []string{"[sudo] password for operator: "}, want: "[sudo] password for operator: "},
		{name: "withheld refusal", withhold: true, writes: []string{"sudo: a password is required\n"}, withheld: true},
		{name: "product diagnostic survives", withhold: true, writes: []string{"[FAIL] context.state: context store is missing registry.json\n"}, want: "[FAIL] context.state: context store is missing registry.json\n"},
		{name: "refresh warning survives", withhold: true, writes: []string{"[WARN] sudo credential refresh stopped; the active operation continues.\n"}, want: "[WARN] sudo credential refresh stopped; the active operation continues.\n"},
		{name: "split writes rejoin one line", withhold: true, writes: []string{"sudo", ": a password", " is required\n"}, withheld: true},
		{name: "refusal before a diagnostic", withhold: true, writes: []string{"sudo: a password is required\n[FAIL] runtime.privilege: denied\n"}, want: "[FAIL] runtime.privilege: denied\n", withheld: true},
		{name: "unterminated diagnostic flushes on close", withhold: true, writes: []string{"[FAIL] cli.usage: bad"}, want: "[FAIL] cli.usage: bad"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var out bytes.Buffer
			writer := &invocationError{writer: &out, withhold: testCase.withhold}
			for _, write := range testCase.writes {
				if n, err := writer.Write([]byte(write)); err != nil || n != len(write) {
					t.Fatalf("write %q = %d, %v", write, n, err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatalf("close = %v", err)
			}
			if out.String() != testCase.want {
				t.Fatalf("stderr = %q, want %q", out.String(), testCase.want)
			}
			if writer.withheld != testCase.withheld {
				t.Fatalf("withheld = %v, want %v", writer.withheld, testCase.withheld)
			}
		})
	}
}

func TestElevatedStderrBoundsOneWithheldLine(t *testing.T) {
	var out bytes.Buffer
	writer := &invocationError{writer: &out, withhold: true}
	overflow := "sudo: " + strings.Repeat("x", 2*invocationErrorLine)
	if _, err := writer.Write([]byte(overflow)); err != nil {
		t.Fatalf("write = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close = %v", err)
	}
	if len(writer.pending) != 0 {
		t.Fatalf("pending retained %d bytes", len(writer.pending))
	}
	// The first bounded segment is a refusal; the remainder cannot reconstruct
	// one, so it reaches the operator rather than disappearing silently.
	if out.Len() == 0 || out.Len() >= len(overflow) {
		t.Fatalf("stderr = %d bytes of %d", out.Len(), len(overflow))
	}
}
