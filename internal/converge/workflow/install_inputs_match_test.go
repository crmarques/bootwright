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
	if installInputsMatch(structuralLess, "full-A", "struct-X") {
		t.Fatal("a current-schema record without structural evidence must not match")
	}

	if installInputsMatch(rec, "full-A", "") {
		t.Fatal("an empty computed structural hash must not match")
	}

	preSchema := ClusterInstallRecord{DesiredHash: "old-full", StructuralHash: "old-struct"}
	if installInputsMatch(preSchema, "new-full", "new-struct") {
		t.Fatal("a pre-schema record must NOT read as a match: bootwright cannot prove what it was installed from, so it fails closed instead of silently adopting the record")
	}
}
