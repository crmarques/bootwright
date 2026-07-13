package workflow

import "testing"

func TestInstallInputsMatch(t *testing.T) {
	rec := ClusterInstallRecord{DesiredHash: "full-A", StructuralHash: "struct-A", HashSchema: ConvergeHashSchema}

	if !installInputsMatch(rec, "full-B", "struct-A") {
		t.Fatal("a day-2-only edit (full hash moved, structural unchanged) must read as a match")
	}
	if installInputsMatch(rec, "full-B", "struct-B") {
		t.Fatal("an install-input edit (structural hash moved) must NOT read as a match")
	}

	structuralLess := ClusterInstallRecord{DesiredHash: "full-A", HashSchema: ConvergeHashSchema}
	if !installInputsMatch(structuralLess, "full-A", "struct-X") {
		t.Fatal("a structural-less record must match on the full desired hash")
	}
	if installInputsMatch(structuralLess, "full-B", "struct-X") {
		t.Fatal("a structural-less record must differ on a changed full desired hash")
	}

	if !installInputsMatch(rec, "full-A", "") {
		t.Fatal("an empty computed structural hash must fall back to the full desired hash")
	}

	preSchema := ClusterInstallRecord{DesiredHash: "old-full", StructuralHash: "old-struct"}
	if !installInputsMatch(preSchema, "new-full", "new-struct") {
		t.Fatal("a pre-schema record must migrate as matching so an upgrade does not re-image an installed cluster")
	}
}
