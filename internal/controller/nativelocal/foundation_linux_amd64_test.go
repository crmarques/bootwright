//go:build linux && amd64

package nativelocal

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// recordedLibgcc is what rpm 6.0.2 printed on a Fedora 43 host on 2026-10-06
// for `LC_ALL=C rpm -q libgcc --queryformat` with foundationQueryFormat: one
// identity line per installed instance, x86_64 then i686, each followed by its
// files, where a directory carries neither digest nor link target and a link
// only its target.
const recordedLibgcc = "libgcc\t(none)\t15.3.1\t1.fc43\tx86_64\tRSA/SHA256, Fri Jul 24 12:23:11 2026, Key ID 829b606631645531\t(none)\t8\n" +
	"/lib64/libgcc_s-15-20260722.so.1\t5c4a8dcab096509ff454c0516cf364ecc9c727f67973d174606a590bd35baf05\t\n" +
	"/lib64/libgcc_s.so.1\t\tlibgcc_s-15-20260722.so.1\n" +
	"/usr/lib/.build-id\t\t\n" +
	"/usr/lib/.build-id/69\t\t\n" +
	"/usr/lib/.build-id/69/a9ebbfaef384c769a565fe62d8734cdb63cfbb\t\t../../../../lib64/libgcc_s-15-20260722.so.1\n" +
	"/usr/share/licenses/libgcc\t\t\n" +
	"/usr/share/licenses/libgcc/COPYING\t231f7edcc7352d7734a96eef0b8030f77982678c516876fcb81e25b32d68564c\t\n" +
	"libgcc\t(none)\t15.3.1\t1.fc43\ti686\tRSA/SHA256, Fri Jul 24 12:22:43 2026, Key ID 829b606631645531\t(none)\t8\n" +
	"/lib/libgcc_s-15-20260722.so.1\td8ec40502a513451eed91fc57acabecb906968f61d3d9564e7a684daf341dbb9\t\n" +
	"/lib/libgcc_s.so.1\t\tlibgcc_s-15-20260722.so.1\n"

// The foundation query reads each instance's identity, whether the platform's
// vendor key signed it, its digest algorithm and every file's digest and link
// target. Another key's signature is not the vendor's, a package with no
// instance has none, an i686 instance stays its own build, and a line outside
// the format refuses rather than reading as less evidence.
func TestFoundationBuildsReadRPMFileDigests(t *testing.T) {
	fedora, err := vendorKeyFor(prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"})
	if err != nil || fedora != "829b606631645531" {
		t.Fatalf("the Fedora 43 vendor key is %q (%v)", fedora, err)
	}
	if rhel, err := vendorKeyFor(prerequisites.Platform{OS: "rhel", Release: "9.8", Architecture: "amd64"}); err != nil || rhel != vendorKeyID {
		t.Fatalf("the RHEL 9.8 vendor key is %q (%v)", rhel, err)
	}
	if _, err := vendorKeyFor(prerequisites.Platform{OS: "rhel", Release: "9.9", Architecture: "amd64"}); err == nil {
		t.Fatal("another minor has a vendor key")
	}
	if !foundationNames([]string{"glibc", "libgcc"}) || foundationNames([]string{"glibc", "glibc"}) || foundationNames([]string{"podman"}) || foundationNames(nil) {
		t.Fatal("the foundation query admits other names")
	}
	t.Run("vendor signed", func(t *testing.T) {
		builds, err := parseFoundationQuery("libgcc", operatorQuery{Stdout: []byte(recordedLibgcc)}, fedora)
		if err != nil || len(builds) != 2 {
			t.Fatalf("parsed %+v (%v)", builds, err)
		}
		x86, i686 := builds[0], builds[1]
		if x86.Name != "libgcc" || x86.Version != "15.3.1" || x86.Release != "1.fc43" || x86.Architecture != "x86_64" || x86.Epoch != 0 || !x86.Signed || x86.DigestAlgorithm != 8 || len(x86.Files) != 7 {
			t.Fatalf("the x86_64 build is %+v", x86)
		}
		if file := x86.Files["/lib64/libgcc_s-15-20260722.so.1"]; file.SHA256 != "5c4a8dcab096509ff454c0516cf364ecc9c727f67973d174606a590bd35baf05" || file.LinkTo != "" {
			t.Fatalf("the versioned library is %+v", file)
		}
		if link := x86.Files["/lib64/libgcc_s.so.1"]; link.SHA256 != "" || link.LinkTo != "libgcc_s-15-20260722.so.1" {
			t.Fatalf("the link is %+v", link)
		}
		if _, found := x86.Files["/lib/libgcc_s.so.1"]; found || i686.Architecture != "i686" || i686.Files["/lib/libgcc_s.so.1"].LinkTo != "libgcc_s-15-20260722.so.1" {
			t.Fatalf("the i686 instance's files entered the x86_64 build: %+v", i686)
		}
	})
	t.Run("a foreign key", func(t *testing.T) {
		foreign := strings.Replace(recordedLibgcc, "Key ID 829b606631645531\t(none)\t8\n/lib64", "Key ID "+vendorKeyID+"\t(none)\t8\n/lib64", 1)
		builds, err := parseFoundationQuery("libgcc", operatorQuery{Stdout: []byte(foreign)}, fedora)
		if err != nil || len(builds) != 2 || builds[0].Signed || !builds[1].Signed {
			t.Fatalf("a build another key signed reads %+v (%v)", builds, err)
		}
	})
	t.Run("a missing instance", func(t *testing.T) {
		builds, err := parseFoundationQuery("glibc", operatorQuery{Stdout: []byte("package glibc is not installed\n"), Exit: 1}, fedora)
		if err != nil || builds == nil || len(builds) != 0 {
			t.Fatalf("an absent package reads %+v (%v)", builds, err)
		}
	})
	for name, query := range map[string]operatorQuery{
		"a file before any build":   {Stdout: []byte("/lib64/libgcc_s.so.1\t\tlibgcc_s-15-20260722.so.1\n")},
		"a file line short a field": {Stdout: []byte(strings.Replace(recordedLibgcc, "/lib64/libgcc_s.so.1\t\t", "/lib64/libgcc_s.so.1\t", 1))},
		"a short digest":            {Stdout: []byte(strings.Replace(recordedLibgcc, "5c4a8dcab096509f", "5c4a", 1))},
		"an identity short a field": {Stdout: []byte(strings.Replace(recordedLibgcc, "\t(none)\t8\n/lib64", "\t(none)\n/lib64", 1))},
		"a malformed algorithm":     {Stdout: []byte(strings.Replace(recordedLibgcc, "\t(none)\t8\n/lib64", "\t(none)\tsha256\n/lib64", 1))},
		"a file listed twice":       {Stdout: []byte(strings.Replace(recordedLibgcc, "/usr/lib/.build-id/69\t\t\n", "/usr/lib/.build-id\t\t\n", 1))},
		"another package's line":    {Stdout: []byte(strings.Replace(recordedLibgcc, "libgcc\t(none)", "glibc\t(none)", 1))},
		"no final newline":          {Stdout: []byte(strings.TrimSuffix(recordedLibgcc, "\n"))},
		"an unreadable database":    {Stdout: []byte("error: cannot open Packages index\n"), Exit: 1},
	} {
		t.Run(name, func(t *testing.T) {
			if builds, err := parseFoundationQuery("libgcc", query, fedora); err == nil {
				t.Fatalf("a malformed query read %+v", builds)
			}
		})
	}
}
