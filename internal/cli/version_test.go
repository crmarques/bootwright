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
			want: "version: devel\ncommit: unknown\ngo: unknown\ntarget: unknown/unknown\ndependency bundle: none\n",
		},
		{
			name: "normalized",
			info: BuildInfo{Version: " v0.1.0 ", Commit: " ABCDEF0 ", GoVersion: " go1.26.7 ", GOOS: " linux ", GOARCH: " amd64 ", DependencyBundle: " sha256:" + strings.Repeat("a", 64) + " "},
			want: "version: v0.1.0\ncommit: abcdef0\ngo: go1.26.7\ntarget: linux/amd64\ndependency bundle: sha256:" + strings.Repeat("a", 64) + "\n",
		},
		{
			name: "unsafe build values",
			info: BuildInfo{Version: "v1\n\x1b[2J", Commit: "not-a-commit", GoVersion: "go\xff", GOOS: "x\\y", DependencyBundle: "sha256:" + strings.Repeat("A", 64)},
			want: "version: v1\\n\\u001b[2J\ncommit: unknown\ngo: go\\xff\ntarget: x\\\\y/unknown\ndependency bundle: none\n",
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
		if !strings.Contains(out.String(), "commit: "+want+"\n") {
			t.Fatalf("commit length %d: %q", n, out.String())
		}
	}
}
