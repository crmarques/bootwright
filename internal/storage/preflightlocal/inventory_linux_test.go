package preflightlocal

import (
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
