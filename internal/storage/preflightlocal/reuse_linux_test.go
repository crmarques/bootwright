package preflightlocal

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/storage/preflight"
)

func TestDomainCreditRequiresExactUUIDNameAndDiskSet(t *testing.T) {
	input := `<domain><name>owned-domain</name><uuid>expected</uuid><devices><disk type="file" device="disk"><source file="/owned/root.qcow2"/></disk></devices></domain>`
	allocation := preflight.Allocation{Domain: "owned-domain", UUID: "expected", Disks: []string{"/owned/root.qcow2"}}
	var domain domainXML
	if err := xml.Unmarshal([]byte(input), &domain); err != nil {
		t.Fatal(err)
	}
	if !domainMatches(domain, allocation) {
		t.Fatal("exact identity refused")
	}
	for _, bad := range []preflight.Allocation{{Domain: "foreign", UUID: "expected", Disks: allocation.Disks}, {Domain: allocation.Domain, UUID: "foreign", Disks: allocation.Disks}, {Domain: allocation.Domain, UUID: allocation.UUID, Disks: []string{"/foreign/root.qcow2"}}} {
		if domainMatches(domain, bad) {
			t.Fatal("foreign domain gained credit")
		}
	}
}
func TestCapacityFilesRefuseSymlinksAndBoundBytes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "proof")
	if err := os.WriteFile(path, []byte("12345"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSafe(path, 4); err == nil {
		t.Fatal("unbounded read")
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readSafe(link, 10); err == nil {
		t.Fatal("followed final symlink")
	}
	directory := filepath.Join(root, "alias")
	if err := os.Symlink(root, directory); err != nil {
		t.Fatal(err)
	}
	if _, err := readSafe(filepath.Join(directory, "proof"), 10); err == nil {
		t.Fatal("followed intermediate symlink")
	}
}
func TestDiskCreditRequiresUniqueFilesAndSameFilesystem(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "disk")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 8192)), 0600); err != nil {
		t.Fatal(err)
	}
	_, identity, err := availableDisk(directory)
	if err != nil {
		t.Fatal(err)
	}
	size, err := diskCredit([]string{path}, identity)
	if err != nil || size <= 0 {
		t.Fatalf("%d %v", size, err)
	}
	if _, err := diskCredit([]string{path, path}, identity); err == nil {
		t.Fatal("double credited inode")
	}
	if _, err := diskCredit([]string{path}, "another filesystem"); err == nil {
		t.Fatal("credited another filesystem")
	}
}
func TestCaptureRefusesExcessWithoutRetainingIt(t *testing.T) {
	capture := &Capture{}
	if _, err := capture.Write(make([]byte, MaxOutput)); err != nil {
		t.Fatal(err)
	}
	if _, err := capture.Write([]byte("secret excess")); err == nil || !capture.Overflow || len(capture.Data) != MaxOutput {
		t.Fatal("capture exceeded bound")
	}
}
