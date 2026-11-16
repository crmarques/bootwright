package machine

import (
	"encoding/json"
	"os"
	"testing"
)

const diskIdentityCorpus = "../../ansible/collections/ansible_collections/bootwright/core/tests/unit/goldens/disk_identity_trees.json"

// ValidDiskSerial accepts exactly the serials the shared disk identity corpus
// lists as valid, the same set the Python readers admit.
func TestValidDiskSerialFollowsTheSharedCorpus(t *testing.T) {
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
		}
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Grammar.Serials) == 0 {
		t.Fatal("the corpus holds no serial")
	}
	for _, entry := range corpus.Grammar.Serials {
		if got := ValidDiskSerial(entry.Value); got != entry.Valid {
			t.Errorf("serial %q: valid %v, corpus says %v", entry.Value, got, entry.Valid)
		}
	}
}
