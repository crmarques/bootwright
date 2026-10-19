package preflightlocal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/storage/preflight"
)

func TestForeignDomainCPUReservationsExcludeOnlyExactProjectedOwners(t *testing.T) {
	domains := map[string]domainXML{"ours": {Name: "owned", UUID: "ours", VCPU: 4}, "foreign": {Name: "another", UUID: "foreign", VCPU: 3}}
	count, err := foreignBudget(domains, []preflight.Allocation{{Domain: "owned", UUID: "ours"}})
	if err != nil || count != 3 {
		t.Fatalf("%d %v", count, err)
	}
	if _, err := foreignBudget(domains, []preflight.Allocation{{Domain: "wrong", UUID: "ours"}}); err == nil {
		t.Fatal("credited foreign UUID holder")
	}
}
func TestInventoryReadsConfiguredAndLiveDomainShapes(t *testing.T) {
	domain := `<domain><name>owned</name><uuid>ours</uuid><vcpu>4</vcpu></domain>`
	for _, data := range []string{domain, `<domstatus pid="123">` + domain + `</domstatus>`} {
		got, err := inventoryDomain([]byte(data))
		if err != nil || got.Name != "owned" || got.UUID != "ours" || got.VCPU != 4 {
			t.Fatalf("%+v %v", got, err)
		}
	}
	for _, data := range []string{`<domain/>`, `<domain><name>x</name><uuid>x</uuid><vcpu>0</vcpu></domain>`, `<unrelated/>`} {
		if _, err := inventoryDomain([]byte(data)); err == nil {
			t.Fatal("unproved inventory admitted")
		}
	}
}
func TestDomainDirectoryReadsAFileLargerThanTheOldBound(t *testing.T) {
	directory := t.TempDir()
	body := `<domain><name>big</name><uuid>u1</uuid><vcpu>2</vcpu><metadata>` + strings.Repeat("x", 100<<10) + `</metadata></domain>`
	if err := os.WriteFile(filepath.Join(directory, "big.xml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	domains := map[string]domainXML{}
	if err := readDomainDirectory(context.Background(), directory, readSafe, domains); err != nil || domains["u1"].VCPU != 2 {
		t.Fatalf("%+v %v", domains, err)
	}
}
func TestDomainDirectoryRefusalNamesTheFile(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "network.xml"), []byte(`<network/>`), 0o600); err != nil {
		t.Fatal(err)
	}
	err := readDomainDirectory(context.Background(), directory, readSafe, map[string]domainXML{})
	if err == nil || !strings.Contains(err.Error(), "network.xml") {
		t.Fatalf("%v", err)
	}
	reason := inventoryReason(err)
	if !strings.HasPrefix(reason, "the libvirt domain inventory could not be read: network.xml: ") {
		t.Fatal(reason)
	}
	if long := inventoryReason(os.ErrInvalid); len(long) > 220 {
		t.Fatal(long)
	}
}
func TestDomainDirectoryBoundIsTheSharedProofBound(t *testing.T) {
	directory := t.TempDir()
	body := `<domain><name>big</name><uuid>u1</uuid><vcpu>2</vcpu><metadata>` + strings.Repeat("x", domainProofBound) + `</metadata></domain>`
	if err := os.WriteFile(filepath.Join(directory, "big.xml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	err := readDomainDirectory(context.Background(), directory, readSafe, map[string]domainXML{})
	if err == nil || !strings.Contains(err.Error(), "big.xml") {
		t.Fatalf("%v", err)
	}
}
func TestUnreadReasonNamesTheInventoryOnlyWhenItIsTheOnlyUnreadFact(t *testing.T) {
	inventory := inventoryReason(errors.New("network.xml: unrecognized libvirt domain inventory"))
	if got := unreadReason(preflight.Facts{MemoryKnown: true, DiskKnown: true}, inventory); got != inventory {
		t.Fatal(got)
	}
	if got := unreadReason(preflight.Facts{DiskKnown: true}, inventory); !strings.Contains(got, "kernel capacity facts") {
		t.Fatal(got)
	}
	if got := unreadReason(preflight.Facts{CPUKnown: true, MemoryKnown: true, DiskKnown: true}, ""); got != "" {
		t.Fatal(got)
	}
}
