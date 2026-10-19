package preflightlocal

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
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
func TestARuntimeProofFailureKeepsTheProvedDiskCredit(t *testing.T) {
	allocation := preflight.Allocation{Machine: "m1", Quantities: preflight.Quantities{Memory: 100, Disk: 1000}}
	credit, err := reuseCredit(allocation, 600, 0, errors.New("the runtime process changed during observation"))
	if err == nil || credit != (preflight.Quantities{Disk: 600}) {
		t.Fatalf("%+v %v", credit, err)
	}
	facts := preflight.Facts{Reusable: map[string]preflight.Quantities{}, Unproved: map[string]string{}}
	recordReuse(&facts, allocation.Machine, credit, err)
	if facts.Reusable["m1"] != credit || facts.Unproved["m1"] != "the runtime process changed during observation" {
		t.Fatalf("%+v", facts)
	}
	credit, err = reuseCredit(allocation, 2000, 300, nil)
	if err != nil || credit != (preflight.Quantities{Memory: 100, Disk: 1000}) {
		t.Fatalf("%+v %v", credit, err)
	}
	facts = preflight.Facts{Reusable: map[string]preflight.Quantities{}, Unproved: map[string]string{}}
	recordReuse(&facts, "m2", preflight.Quantities{}, errors.New("the domain does not prove its projected identity and disks"))
	if _, credited := facts.Reusable["m2"]; credited || facts.Unproved["m2"] == "" {
		t.Fatalf("an unproved definition gained credit: %+v", facts)
	}
	recordReuse(&facts, "m3", preflight.Quantities{}, errors.New(strings.Repeat("x", 400)))
	if len(facts.Unproved["m3"]) > 160 {
		t.Fatal("unbounded proof reason")
	}
}
func TestReusableKeepsTheDiskCreditOnlyWhenTheDefinitionAndDisksAreProved(t *testing.T) {
	allocation := preflight.Allocation{Machine: "m1", Domain: "owned-domain", UUID: "expected", Disks: []string{"/owned/root.qcow2"}, Allocated: true, Quantities: preflight.Quantities{Memory: 100, Disk: 1000}}
	definition := []byte(`<domain><name>owned-domain</name><uuid>expected</uuid><devices><disk type="file" device="disk"><source file="/owned/root.qcow2"/></disk></devices></domain>`)
	config := func(data []byte, err error) func(string, int64) ([]byte, error) {
		return func(path string, limit int64) ([]byte, error) {
			if path != "/etc/libvirt/qemu/owned-domain.xml" || limit != domainProofBound {
				return nil, fmt.Errorf("unexpected proof %s %d", path, limit)
			}
			return data, err
		}
	}
	quantity := func(amount int64, err error) func(preflight.Allocation) (int64, error) {
		return func(preflight.Allocation) (int64, error) { return amount, err }
	}
	unreached := func(preflight.Allocation) (int64, error) {
		t.Error("a later proof ran after an earlier one failed")
		return 0, nil
	}
	cases := []struct {
		name    string
		proofs  reuseProofs
		credit  preflight.Quantities
		failed  bool
		runtime bool
	}{
		{"undefined domain", reuseProofs{config(nil, os.ErrNotExist), unreached, unreached}, preflight.Quantities{}, false, false},
		{"unreadable definition", reuseProofs{config(nil, errors.New("domain proof is not protected root-owned state")), unreached, unreached}, preflight.Quantities{}, true, false},
		{"foreign definition", reuseProofs{config([]byte(`<domain><name>owned-domain</name><uuid>foreign</uuid></domain>`), nil), unreached, unreached}, preflight.Quantities{}, true, false},
		{"unproved disks", reuseProofs{config(definition, nil), quantity(0, errors.New("projected disks alias one file")), unreached}, preflight.Quantities{}, true, false},
		{"unproved runtime", reuseProofs{config(definition, nil), quantity(600, nil), quantity(0, errors.New("the runtime process changed during observation"))}, preflight.Quantities{Disk: 600}, true, true},
		{"unproved runtime over the declared disk", reuseProofs{config(definition, nil), quantity(4000, nil), quantity(0, errors.New("anonymous resident memory is unknown"))}, preflight.Quantities{Disk: 1000}, true, true},
		{"proved", reuseProofs{config(definition, nil), quantity(600, nil), quantity(300, nil)}, preflight.Quantities{Memory: 100, Disk: 600}, false, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			credit, err := test.proofs.reusable(context.Background(), allocation)
			var runtime runtimeUnproved
			if credit != test.credit || (err != nil) != test.failed || errors.As(err, &runtime) != test.runtime {
				t.Fatalf("%+v %v", credit, err)
			}
		})
	}
}
