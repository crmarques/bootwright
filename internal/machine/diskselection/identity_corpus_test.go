package diskselection

import (
	"encoding/json"
	"os"
	"testing"
)

const diskIdentityCorpus = "../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/goldens/disk_identity_trees.json"

// The disk identity grammar is one corpus the Go validators and the Python
// readers both read: DiskIdentity.Validate accepts exactly its serials, sizes
// and WWNs.
func TestDiskIdentityValidationFollowsTheSharedCorpus(t *testing.T) {
	data, err := os.ReadFile(diskIdentityCorpus)
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Grammar struct {
			Serials []struct {
				Value string
				Valid bool
			}
			Sizes []struct {
				Bytes int64
				Valid bool
			}
			WWNs []struct {
				WWN   string `json:"wwn"`
				Valid bool
			} `json:"wwns"`
		}
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Grammar.Serials) == 0 || len(corpus.Grammar.Sizes) == 0 || len(corpus.Grammar.WWNs) == 0 {
		t.Fatal("the corpus grammar is empty")
	}
	for _, entry := range corpus.Grammar.Serials {
		if got := (DiskIdentity{SerialNumber: entry.Value, SizeBytes: 4096}).Validate() == nil; got != entry.Valid {
			t.Errorf("serial %q: valid %v, corpus says %v", entry.Value, got, entry.Valid)
		}
	}
	for _, entry := range corpus.Grammar.Sizes {
		if got := (DiskIdentity{SerialNumber: "ZC1A2B3C", SizeBytes: entry.Bytes}).Validate() == nil; got != entry.Valid {
			t.Errorf("size %d: valid %v, corpus says %v", entry.Bytes, got, entry.Valid)
		}
	}
	for _, entry := range corpus.Grammar.WWNs {
		if got := (DiskIdentity{WWN: entry.WWN, SizeBytes: 4096}).Validate() == nil; got != entry.Valid {
			t.Errorf("wwn %q: valid %v, corpus says %v", entry.WWN, got, entry.Valid)
		}
	}
}
