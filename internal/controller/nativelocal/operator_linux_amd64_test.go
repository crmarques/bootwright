//go:build linux && amd64

package nativelocal

import (
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// The query lines below have the shape rpm's pgpsig format prints, observed on
// 2026-10-06 with `rpm -q --queryformat` on a Fedora 43 host (rpm 6.0.2):
//
//	xorriso	(none)	1.5.8	2.fc43	x86_64	RSA/SHA256, Mon May 25 13:19:07 2026, Key ID 829b606631645531	(none)
//
// where 829b606631645531 is the low 64 bits of the Fedora 43 signer this
// package's Fedora profile qualifies, and a package with no instance exits 1
// printing "package <name> is not installed" on standard output alone. The
// RHEL 9 lines carry the RHEL 9 profile's own key in the same position.
func queryLine(name, release, header, payload string) string {
	return name + "\t(none)\t1.0\t" + release + "\tx86_64\t" + header + "\t" + payload + "\n"
}

func signature(key string) string { return "RSA/SHA256, Tue Mar 15 06:10:30 2022, Key ID " + key }

// fedoraKeyID is the key ID of the Fedora 43 signer, as rpm prints it.
var fedoraKeyID = "c6e7f081cf80e13146676e88829b606631645531"[24:]

// Only instances that a signature of the RHEL 9 vendor key covers, and no
// other key, are accepted, and a package with no instance is missing.
func TestOperatorRootsAcceptOnlyTheVendorKey(t *testing.T) {
	if vendorKeyID != "199e2f91fd431d51" || !strings.HasSuffix(rhel9Signer, vendorKeyID) {
		t.Fatalf("vendor key ID = %q", vendorKeyID)
	}
	signed := operatorQuery{Stdout: []byte(queryLine("lorax", "1.el9", signature(vendorKeyID), "(none)"))}
	for _, test := range []struct {
		name             string
		xorriso          operatorQuery
		missing, foreign []string
	}{
		{name: "vendor signed", xorriso: operatorQuery{Stdout: []byte(queryLine("xorriso", "5.el9", signature(vendorKeyID), signature(vendorKeyID)))}},
		{name: "unsigned", xorriso: operatorQuery{Stdout: []byte(queryLine("xorriso", "5.el9", "(none)", "(none)"))}, foreign: []string{"xorriso"}},
		{name: "signed by another key", xorriso: operatorQuery{Stdout: []byte(queryLine("xorriso", "2.fc43", signature(fedoraKeyID), "(none)"))}, foreign: []string{"xorriso"}},
		{name: "also signed by another key", xorriso: operatorQuery{Stdout: []byte(queryLine("xorriso", "5.el9", signature(vendorKeyID), signature(fedoraKeyID)))}, foreign: []string{"xorriso"}},
		{name: "not installed", xorriso: operatorQuery{Stdout: []byte("package xorriso is not installed\n"), Exit: 1}, missing: []string{"xorriso"}},
		{name: "two instances, one foreign", xorriso: operatorQuery{Stdout: []byte(queryLine("xorriso", "5.el9", signature(vendorKeyID), "(none)") + queryLine("xorriso", "6.el9", "(none)", "(none)"))}, foreign: []string{"xorriso"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			presence, err := operatorPresence([]string{"lorax", "xorriso"}, map[string]operatorQuery{"lorax": signed, "xorriso": test.xorriso}, vendorKeyID)
			if err != nil {
				t.Fatal(err)
			}
			ready := len(test.missing) == 0 && len(test.foreign) == 0
			if presence.Ready != ready || !slices.Equal(presence.Missing, append([]string{}, test.missing...)) || !slices.Equal(presence.Foreign, append([]string{}, test.foreign...)) {
				t.Fatalf("presence = %+v", presence)
			}
			want := 2
			if !ready {
				want = 1
			}
			if len(presence.Installed) != want || presence.Installed[0] != (prerequisites.NativeRootPresence{Key: "installer-media", Package: prerequisites.NativeIdentity{Name: "lorax", Version: "1.0", Release: "1.el9", Architecture: "x86_64"}}) {
				t.Fatalf("installed = %+v", presence.Installed)
			}
		})
	}
}

// A refusal rpm prints on standard error, or anything but the query format's
// own lines, is a database the inspection could not read, never an absence.
func TestOperatorRootsRefuseUnreadableEvidence(t *testing.T) {
	for name, query := range map[string]operatorQuery{
		"a database it cannot open": {Stdout: []byte("package xorriso is not installed\n"), Stderr: []byte("error: cannot open Packages database in /nonexistent/var/lib/rpm\n"), Exit: 1},
		"another refusal":           {Stdout: []byte("error: rpmdb open failed\n"), Exit: 1},
		"another package's line":    {Stdout: []byte(queryLine("lorax", "1.el9", signature(vendorKeyID), "(none)"))},
		"a truncated line":          {Stdout: []byte("xorriso\t(none)\t1.0\n")},
		"no final newline":          {Stdout: []byte(strings.TrimSuffix(queryLine("xorriso", "5.el9", signature(vendorKeyID), "(none)"), "\n"))},
		"an unreadable signature":   {Stdout: []byte(queryLine("xorriso", "5.el9", "(not an OpenPGP signature)", "(none)"))},
		"a malformed epoch":         {Stdout: []byte(strings.Replace(queryLine("xorriso", "5.el9", signature(vendorKeyID), "(none)"), "(none)", "01", 1))},
	} {
		t.Run(name, func(t *testing.T) {
			if presence, err := operatorPresence([]string{"xorriso"}, map[string]operatorQuery{"xorriso": query}, vendorKeyID); err == nil {
				t.Fatalf("accepted as %+v", presence)
			}
		})
	}
	if operatorNames([]string{"lorax", "lorax"}) || operatorNames([]string{"podman"}) || operatorNames(nil) || !operatorNames([]string{"xorriso", "lorax"}) {
		t.Fatal("operator roots admit names other than the installer-media roots, each once")
	}
}

// UBI carries neither lorax nor xorriso, so the RHEL profile never resolves
// them: it refuses with the operator's step, which the stage that meets it
// follows with its own command.
func TestRHELProfileNeverResolvesInstallerMedia(t *testing.T) {
	_, err := profiles(rhelPlatform(), prerequisites.NativeRequirements{ContainerRuntime: true, InstallerMedia: true})
	reported := diagnostics.Of(prerequisites.InStage(err, "lab"))
	want := "Install lorax and xorriso from this host's enabled Red Hat repositories (dnf install lorax xorriso), then run bootwright apply --stage controller --context lab."
	if len(reported) != 1 || reported[0].Code != "controller.unsupported" || reported[0].Remediation != want {
		t.Fatalf("refusal = %+v", reported)
	}
	for _, requirements := range []prerequisites.NativeRequirements{{ContainerRuntime: true, LibvirtClient: true}, {ContainerRuntime: true, Hypervisor: true}} {
		if _, err := profiles(rhelPlatform(), requirements); len(diagnostics.Of(err)) != 1 || diagnostics.Of(err)[0].Code != "controller.unsupported" {
			t.Fatalf("%+v was resolved on RHEL: %v", requirements, err)
		}
	}
	if repositories, err := profiles(rhelPlatform(), prerequisites.NativeRequirements{ContainerRuntime: true}); err != nil || len(repositories) != 2 || repositories[0].Signer != rhel9Signer {
		t.Fatalf("baseline profile = %+v (%v)", repositories, err)
	}
}
