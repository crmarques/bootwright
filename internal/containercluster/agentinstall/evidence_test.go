package agentinstall

import (
	"encoding/json"
	"testing"
)

// The installation adapter records whether a wait of its apply put the kept
// kubeconfig back in place of the installer's own, which no longer parsed.
// Evidence that names the restore proves completion as any other does, and
// evidence an earlier build published without the field reads as no restore.
func TestInstallEvidenceRecordsARestoreOfTheInstallersKubeconfig(t *testing.T) {
	_, install, _ := onlyRequests(t, singleNodeCatalog())
	const completed = `{"absent":false,"cluster":"` + anchorIdentity + `","completed":true,"identity":"` +
		anchorIdentity + `","media":[],"missing":[],"ownMedia":[],"postcondition":true,"powered":["sno-01"],` +
		`"release":"4.21.15","request":"` + testDigest + `"`
	for data, restored := range map[string]bool{
		completed + `,"restored":true}`:  true,
		completed + `,"restored":false}`: false,
		completed + `}`:                  false,
	} {
		if err := ValidateInstallPresence([]byte(data), install, testDigest); err != nil {
			t.Fatalf("the completion evidence %s was refused: %v", data, err)
		}
		evidence, err := decodeInstallEvidence([]byte(data), testDigest)
		if err != nil || evidence.Restored != restored {
			t.Errorf("evidence %s read restored %v (%v), want %v", data, evidence.Restored, err, restored)
		}
	}
}

func TestEvidenceFollowedByAClosingDelimiterIsRefused(t *testing.T) {
	media, err := json.Marshal(MediaEvidence{Absent: true, Postcondition: true, Request: testDigest})
	if err != nil {
		t.Fatal(err)
	}
	install, err := json.Marshal(InstallEvidence{
		Absent: true, Cluster: anchorIdentity, Completed: true, Identity: anchorIdentity,
		Media: []string{}, Missing: []string{}, OwnMedia: []string{}, Postcondition: true,
		Powered: []string{"sno-01"}, Release: "4.21.15", Request: testDigest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if ValidateMediaAbsence(media, testDigest) != nil || ValidateInstallAbsence(install, testDigest) != nil {
		t.Fatal("the removal evidence was refused")
	}
	for _, closer := range []string{"}", "]"} {
		if err := ValidateMediaAbsence([]byte(string(media)+closer), testDigest); err == nil {
			t.Errorf("boot-media evidence followed by %s was accepted", closer)
		}
		if err := ValidateInstallAbsence([]byte(string(install)+closer), testDigest); err == nil {
			t.Errorf("installation evidence followed by %s was accepted", closer)
		}
	}
}
