package workflow

import "testing"

func TestInstallInputsMatch(t *testing.T) {
	rec := ClusterInstallRecord{DesiredHash: "full-A", StructuralHash: "struct-A"}

	if !installInputsMatch(rec, "full-B", "struct-A") {
		t.Fatal("a day-2-only edit (full hash moved, structural unchanged) must read as a match")
	}
	if installInputsMatch(rec, "full-B", "struct-B") {
		t.Fatal("an install-input edit (structural hash moved) must NOT read as a match")
	}

	legacy := ClusterInstallRecord{DesiredHash: "full-A"}
	if !installInputsMatch(legacy, "full-A", "struct-X") {
		t.Fatal("legacy record must match on the full desired hash")
	}
	if installInputsMatch(legacy, "full-B", "struct-X") {
		t.Fatal("legacy record must differ on a changed full desired hash")
	}

	if !installInputsMatch(rec, "full-A", "") {
		t.Fatal("an empty computed structural hash must fall back to the full desired hash")
	}
}
