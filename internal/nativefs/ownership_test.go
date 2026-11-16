package nativefs

import (
	"errors"
	"io/fs"
	"syscall"
	"testing"
	"time"
)

type ownedEntry struct {
	mode fs.FileMode
	uid  uint32
}

func (e ownedEntry) Name() string       { return "entry" }
func (e ownedEntry) Size() int64        { return 0 }
func (e ownedEntry) Mode() fs.FileMode  { return e.mode }
func (e ownedEntry) ModTime() time.Time { return time.Time{} }
func (e ownedEntry) IsDir() bool        { return e.mode.IsDir() }
func (e ownedEntry) Sys() any           { return &syscall.Stat_t{Uid: e.uid} }

const ownedDirectory = fs.ModeDir | 0o755

func ownedTree(entries map[string]ownedEntry, seen *[]string) func(string) (fs.FileInfo, error) {
	return func(path string) (fs.FileInfo, error) {
		if seen != nil {
			*seen = append(*seen, path)
		}
		if found, ok := entries[path]; ok {
			return found, nil
		}
		if path == "/opt/tool" {
			return ownedEntry{0o755, 0}, nil
		}
		return ownedEntry{ownedDirectory, 0}, nil
	}
}

func TestEveryStickyPolicyDecidesAWorldWritableStickyDirectory(t *testing.T) {
	sticky := fs.ModeDir | fs.ModeSticky | 0o777
	for _, row := range []struct {
		name   string
		policy Sticky
		owner  uint32
		want   Verdict
	}{
		{"refused even when root owns it", StickyRefused, 0, Writable},
		{"root-owned admitted under the root policy", StickyRootOwned, 0, Admitted},
		{"account-owned refused under the root policy", StickyRootOwned, 1000, Writable},
		{"account-owned admitted under the trusted-owner policy", StickyTrustedOwner, 1000, Admitted},
		{"root-owned admitted under the trusted-owner policy", StickyTrustedOwner, 0, Admitted},
	} {
		t.Run(row.name, func(t *testing.T) {
			rule := Ownership{Owners: RootOrAccountOwner(1000), Sticky: row.policy}
			if got := rule.Admits(row.owner, sticky); got != row.want {
				t.Fatalf("Admits = %d, want %d", got, row.want)
			}
		})
	}
}

func TestTheStickyExemptionHoldsOnlyForADirectory(t *testing.T) {
	for _, policy := range []Sticky{StickyRefused, StickyRootOwned, StickyTrustedOwner} {
		if got := (Ownership{Owners: RootOwner, Sticky: policy}).Admits(0, fs.ModeSticky|0o777); got != Writable {
			t.Errorf("policy %d admitted a sticky world-writable file: %d", policy, got)
		}
		if got := (Ownership{Owners: RootOwner, Sticky: policy}).Admits(0, fs.ModeDir|0o757); got != Writable {
			t.Errorf("policy %d admitted a world-writable ownedDirectory without the sticky bit: %d", policy, got)
		}
	}
}

func TestAnOwnerOutsideTheTrustedSetRefusesBeforeItsMode(t *testing.T) {
	for _, rule := range []Ownership{{Owners: RootOwner}, {Owners: AccountOwner(1000)}, {}} {
		if got := rule.Admits(1001, fs.ModeDir|fs.ModeSticky|0o777); got != ForeignOwner {
			t.Errorf("an untrusted owner gave %d", got)
		}
	}
	if got := (Ownership{Owners: AccountOwner(1000)}).Admits(1000, fs.ModeDir|0o750); got != Admitted {
		t.Errorf("the trusted account's private ownedDirectory gave %d", got)
	}
	if got := (Ownership{Owners: RootOwner}).AdmitsInfo(nil); got != Unavailable {
		t.Errorf("a missing entry gave %d", got)
	}
}

func TestWalkProvesThePathAndEveryAncestorToRoot(t *testing.T) {
	var seen []string
	leaf, err := Ownership{Owners: RootOwner, Lstat: ownedTree(nil, &seen)}.Walk("/opt/tool")
	if err != nil || leaf == nil || !leaf.Mode().IsRegular() {
		t.Fatalf("Walk = %v, %v", leaf, err)
	}
	if want := []string{"/opt/tool", "/opt", "/"}; len(seen) != len(want) || seen[0] != want[0] || seen[1] != want[1] || seen[2] != want[2] {
		t.Fatalf("visited %q, want %q", seen, want)
	}
}

func TestWalkStopsAtItsTop(t *testing.T) {
	var seen []string
	entries := map[string]ownedEntry{"/": {fs.ModeDir | 0o777, 1000}}
	if _, err := (Ownership{Owners: RootOwner, Top: "/opt", Lstat: ownedTree(entries, &seen)}).Walk("/opt/tool"); err != nil || len(seen) != 2 {
		t.Fatalf("Walk below its top = %v after %q", err, seen)
	}
}

func TestWalkNamesTheFirstEntryThatFails(t *testing.T) {
	for _, row := range []struct {
		name    string
		entries map[string]ownedEntry
		path    string
		uid     uint32
		verdict Verdict
	}{
		{"foreign leaf", map[string]ownedEntry{"/opt/tool": {0o755, 1000}}, "/opt/tool", 1000, ForeignOwner},
		{"writable ancestor", map[string]ownedEntry{"/opt": {fs.ModeDir | 0o775, 0}}, "/opt", 0, Writable},
		{"foreign root", map[string]ownedEntry{"/": {ownedDirectory, 7}}, "/", 7, ForeignOwner},
		{"ancestor that is a link", map[string]ownedEntry{"/opt": {fs.ModeSymlink | 0o777, 0}}, "/opt", 0, NotDirectory},
	} {
		t.Run(row.name, func(t *testing.T) {
			_, err := Ownership{Owners: RootOwner, Lstat: ownedTree(row.entries, nil)}.Walk("/opt/tool")
			var fault *OwnershipFault
			if !errors.As(err, &fault) || fault.Path != row.path || fault.UID != row.uid || fault.Verdict != row.verdict {
				t.Fatalf("Walk = %#v, want %s uid %d reason %d", err, row.path, row.uid, row.verdict)
			}
		})
	}
}

func TestWalkRefusesARelativeOrMissingPath(t *testing.T) {
	missing := func(string) (fs.FileInfo, error) { return nil, fs.ErrNotExist }
	for _, path := range []string{"opt/tool", "/opt/tool"} {
		var fault *OwnershipFault
		if _, err := (Ownership{Owners: RootOwner, Lstat: missing}).Walk(path); !errors.As(err, &fault) || fault.Verdict != Unavailable {
			t.Errorf("Walk(%q) = %v", path, err)
		}
	}
}

func TestAFaultNamesItsPathAndReason(t *testing.T) {
	for _, row := range []struct {
		fault OwnershipFault
		want  string
	}{
		{OwnershipFault{Path: "/opt", UID: 7, Verdict: ForeignOwner}, "/opt is owned by uid 7"},
		{OwnershipFault{Path: "/opt", Verdict: Writable}, "/opt is writable by group or others"},
		{OwnershipFault{Path: "/opt", Verdict: NotDirectory}, "/opt is not a directory"},
		{OwnershipFault{Path: "/opt", Verdict: Unavailable}, "/opt is unavailable"},
	} {
		if got := row.fault.Error(); got != row.want {
			t.Errorf("Error = %q, want %q", got, row.want)
		}
	}
}

func TestRawModeCarriesTheDirectoryStickyAndPermissionBits(t *testing.T) {
	if got := RawMode(syscall.S_IFDIR | syscall.S_ISVTX | 0o777); got != fs.ModeDir|fs.ModeSticky|0o777 {
		t.Errorf("RawMode = %v", got)
	}
	if got := RawMode(syscall.S_IFREG | 0o644); got != 0o644 {
		t.Errorf("RawMode = %v", got)
	}
}
