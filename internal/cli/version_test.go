package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionOutput(t *testing.T) {
	tests := []struct {
		name string
		info BuildInfo
		want string
	}{
		{
			name: "defaults",
			want: "Bootwright\n\n  Version            devel\n  Commit             unknown\n  Source             unknown\n  Go                 unknown\n  Target             unknown/unknown\n  Dependency bundle  none\n",
		},
		{
			name: "normalized",
			info: BuildInfo{Version: " v0.1.0 ", Commit: " ABCDEF0 ", Source: " CLEAN ", GoVersion: " go1.26.7 ", GOOS: " linux ", GOARCH: " amd64 ", DependencyBundle: " sha256:" + strings.Repeat("a", 64) + " "},
			want: "Bootwright\n\n  Version            v0.1.0\n  Commit             abcdef0\n  Source             clean\n  Go                 go1.26.7\n  Target             linux/amd64\n  Dependency bundle  sha256:" + strings.Repeat("a", 64) + "\n",
		},
		{
			name: "unsafe build values",
			info: BuildInfo{Version: "v1\n\x1b[2J", Commit: "not-a-commit", Source: "dirty", GoVersion: "go\xff", GOOS: "x\\y", DependencyBundle: "sha256:" + strings.Repeat("A", 64)},
			want: "Bootwright\n\n  Version            v1\\n\\u001b[2J\n  Commit             unknown\n  Source             unknown\n  Go                 go\\xff\n  Target             x\\\\y/unknown\n  Dependency bundle  none\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := writeVersion(&out, tt.info); err != nil {
				t.Fatal(err)
			}
			if out.String() != tt.want {
				t.Fatalf("output = %q, want %q", out.String(), tt.want)
			}
		})
	}
}

func TestVersionSourceState(t *testing.T) {
	for _, test := range []struct{ supplied, want string }{
		{supplied: "clean", want: "clean"},
		{supplied: " MODIFIED ", want: "modified"},
		{supplied: "", want: "unknown"},
		{supplied: "dirty", want: "unknown"},
		{supplied: "clean\n  Commit             0000000", want: "unknown"},
	} {
		var out bytes.Buffer
		if err := writeVersion(&out, BuildInfo{Source: test.supplied}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "Source             "+test.want+"\n") {
			t.Fatalf("source %q: %q", test.supplied, out.String())
		}
	}
}

func TestVersionIdentityBounds(t *testing.T) {
	for _, n := range []int{0, 6, 7, 64, 65} {
		var out bytes.Buffer
		commit := strings.Repeat("a", n)
		if err := writeVersion(&out, BuildInfo{Commit: commit}); err != nil {
			t.Fatal(err)
		}
		want := "unknown"
		if n >= 7 && n <= 64 {
			want = commit
		}
		if !strings.Contains(out.String(), "Commit             "+want+"\n") {
			t.Fatalf("commit length %d: %q", n, out.String())
		}
	}
}
