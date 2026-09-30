package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func createCompletionFiles(t *testing.T, directory string, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(directory, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func createCompletionDirectories(t *testing.T, directory string, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := os.Mkdir(filepath.Join(directory, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCompletionPathsOfferAtMost256Candidates(t *testing.T) {
	for _, test := range []struct {
		entries, want int
	}{{256, 256}, {257, 256}, {300, 256}} {
		t.Run(fmt.Sprint(test.entries), func(t *testing.T) {
			directory := t.TempDir()
			created := map[string]bool{}
			for index := range test.entries {
				name := fmt.Sprintf("entry-%03d", index)
				createCompletionFiles(t, directory, name)
				created[directory+"/"+name] = true
			}
			got := completionPaths("file", directory+"/")
			if len(got) != test.want || !slices.IsSorted(got) || len(slices.Compact(slices.Clone(got))) != len(got) {
				t.Fatalf("%d entries offered %d candidates, want %d sorted and distinct", test.entries, len(got), test.want)
			}
			for _, candidate := range got {
				if !created[candidate] {
					t.Fatalf("offered %q, which is no entry of the directory", candidate)
				}
			}
		})
	}
}

// The directory is padded with repeated separators, which name the same
// directory, so the prefix length varies while the directory stays readable:
// only the bound, never ENAMETOOLONG, can refuse the 4097-byte prefix.
func TestCompletionPathsRefuseAnOverlongOrMultilinePrefix(t *testing.T) {
	directory := t.TempDir()
	name := strings.Repeat("n", 200)
	createCompletionFiles(t, directory, name)
	partial := name[:128]
	for _, test := range []struct {
		length int
		offers bool
	}{{4096, true}, {4097, false}} {
		t.Run(fmt.Sprint(test.length), func(t *testing.T) {
			padded := directory + strings.Repeat("/", test.length-len(directory)-len(partial))
			prefix := padded + partial
			if len(prefix) != test.length {
				t.Fatalf("prefix is %d bytes, want %d", len(prefix), test.length)
			}
			var want []string
			if test.offers {
				want = []string{padded + name}
			}
			if got := completionPaths("file", prefix); !reflect.DeepEqual(got, want) {
				t.Fatalf("a %d-byte prefix offered %d candidates, want %d", test.length, len(got), len(want))
			}
		})
	}
	for _, breaking := range []string{"line\nbreak", "carriage\rreturn"} {
		createCompletionDirectories(t, directory, breaking)
		createCompletionFiles(t, filepath.Join(directory, breaking), "entry")
		if got := completionPaths("file", directory+"/"+breaking+"/"); got != nil {
			t.Fatalf("a prefix carrying a line break offered %q", got)
		}
	}
}

func TestCompletionPathsOfferDotEntriesOnlyWhenNamed(t *testing.T) {
	directory := t.TempDir()
	createCompletionFiles(t, directory, "visible", ".hidden")
	createCompletionDirectories(t, directory, "shown", ".config")
	createCompletionFiles(t, filepath.Join(directory, ".config"), "settings", ".secret")
	for _, test := range []struct {
		kind, prefix string
		want         []string
	}{
		{"file", "/", []string{"shown/", "visible"}},
		{"file", "/v", []string{"visible"}},
		{"file", "/.", []string{".config/", ".hidden"}},
		{"file", "/.h", []string{".hidden"}},
		{"directory", "/", []string{"shown/"}},
		{"directory", "/.", []string{".config/"}},
		{"file", "/.config/", []string{".config/settings"}},
		{"file", "/.config/.", []string{".config/.secret"}},
	} {
		t.Run(test.kind+" "+test.prefix, func(t *testing.T) {
			var want []string
			for _, candidate := range test.want {
				want = append(want, directory+"/"+candidate)
			}
			if got := completionPaths(test.kind, directory+test.prefix); !reflect.DeepEqual(got, want) {
				t.Fatalf("candidates = %q; want %q", got, want)
			}
		})
	}
}

func TestCompletionPathsWithholdUnsafeNames(t *testing.T) {
	directory := t.TempDir()
	safe := []string{"input.yaml", "naïve", "with-dash_under.score", strings.Repeat("l", 255)}
	createCompletionFiles(t, directory, safe...)
	for _, character := range "\x01\x1b\x1f\x7f\t\n\r \\\"'`$&|;<>*?[](){}!~#" {
		createCompletionFiles(t, directory, "unsafe"+string(character)+"name")
	}
	var want []string
	for _, name := range safe {
		want = append(want, directory+"/"+name)
	}
	slices.Sort(want)
	if got := completionPaths("file", directory+"/"); !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %q; want %q", got, want)
	}
	if got := completionPaths("file", directory+"/unsafe"); got != nil {
		t.Fatalf("unsafe entries offered %q", got)
	}
}

// Linux refuses a file name over 255 bytes, so the entry bound beyond it is
// exercised on the name itself.
func TestSafeCompletionEntryBoundsItsLength(t *testing.T) {
	for _, test := range []struct {
		name string
		want bool
	}{
		{"", false},
		{strings.Repeat("n", 255), true},
		{strings.Repeat("n", 256), false},
	} {
		if got := safeCompletionEntry(test.name); got != test.want {
			t.Fatalf("a %d-byte name is safe = %t; want %t", len(test.name), got, test.want)
		}
	}
}
