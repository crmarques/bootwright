//go:build linux && amd64

package nativelocal

import (
	"strings"
	"testing"
)

// inventoryReport is the native helper's inventory operation's report, the
// canonical JSON that inspect() in the collection's native_resolution.py
// writes with no plan to compare: every installed package identity, sorted,
// the digest resolution records as a plan's beforeSHA256, and rootsReady.
func inventoryReport(identities, digest, ready string) []byte {
	return []byte(`{"inventory":[` + identities + `],"inventorySHA256":"` + digest + `","rootsReady":` + ready + `}`)
}

const (
	bashIdentity  = `{"architecture":"x86_64","epoch":0,"name":"bash","release":"1.fc43","version":"5.3.0"}`
	glibcIdentity = `{"architecture":"x86_64","epoch":0,"name":"glibc","release":"16.fc43","version":"2.42"}`
)

// The inventory digest is read only from the helper's exact report: unknown
// or missing members, trailing data, an empty or malformed identity, a digest
// that is not lowercase SHA-256, and roots reported not ready all refuse.
func TestTheInventoryReportIsDecodedStrictly(t *testing.T) {
	digest := strings.Repeat("d", 64)
	got, err := decodeInventory(inventoryReport(bashIdentity+","+glibcIdentity, digest, "true"))
	if err != nil || got != digest {
		t.Fatalf("the helper's report decoded to %q (%v)", got, err)
	}
	for name, report := range map[string][]byte{
		"an unknown member":      []byte(`{"inventory":[` + bashIdentity + `],"inventorySHA256":"` + digest + `","rootsReady":true,"extra":1}`),
		"an unknown identity":    inventoryReport(`{"architecture":"x86_64","epoch":0,"name":"bash","release":"1.fc43","version":"5.3.0","vendor":"x"}`, digest, "true"),
		"trailing data":          append(inventoryReport(bashIdentity, digest, "true"), []byte(`{}`)...),
		"no identity":            inventoryReport("", digest, "true"),
		"an unnamed identity":    inventoryReport(`{"architecture":"x86_64","epoch":0,"name":"","release":"1.fc43","version":"5.3.0"}`, digest, "true"),
		"a negative epoch":       inventoryReport(`{"architecture":"x86_64","epoch":-1,"name":"bash","release":"1.fc43","version":"5.3.0"}`, digest, "true"),
		"an uppercase digest":    inventoryReport(bashIdentity, strings.Repeat("D", 64), "true"),
		"a short digest":         inventoryReport(bashIdentity, strings.Repeat("d", 63), "true"),
		"roots not ready":        inventoryReport(bashIdentity, digest, "false"),
		"a missing digest":       []byte(`{"inventory":[` + bashIdentity + `],"rootsReady":true}`),
		"an inventory as object": []byte(`{"inventory":{},"inventorySHA256":"` + digest + `","rootsReady":true}`),
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := decodeInventory(report); err == nil {
				t.Fatalf("decoded %q from %s", got, report)
			}
		})
	}
}
