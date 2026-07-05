package workflow

import "testing"

// installInputsMatch is the migration-safe healthy-skip predicate: it compares on
// the day-2-projected structural hash when both the record and the freshly computed
// value carry one (so an add-on / node-label edit reads as a match), and falls back
// to the full desired hash otherwise (a legacy record, or a caller that could not
// compute a structural hash) so the gate never loosens on missing data.
func TestInstallInputsMatch(t *testing.T) {
	rec := ClusterInstallRecord{DesiredHash: "full-A", StructuralHash: "struct-A"}

	if !installInputsMatch(rec, "full-B", "struct-A") {
		t.Fatal("a day-2-only edit (full hash moved, structural unchanged) must read as a match")
	}
	if installInputsMatch(rec, "full-B", "struct-B") {
		t.Fatal("an install-input edit (structural hash moved) must NOT read as a match")
	}

	// Legacy record with no structural hash falls back to the full desired hash.
	legacy := ClusterInstallRecord{DesiredHash: "full-A"}
	if !installInputsMatch(legacy, "full-A", "struct-X") {
		t.Fatal("legacy record must match on the full desired hash")
	}
	if installInputsMatch(legacy, "full-B", "struct-X") {
		t.Fatal("legacy record must differ on a changed full desired hash")
	}

	// A caller that could not compute a structural hash also falls back to full.
	if !installInputsMatch(rec, "full-A", "") {
		t.Fatal("an empty computed structural hash must fall back to the full desired hash")
	}
}
