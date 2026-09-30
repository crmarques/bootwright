package agentinstall

import (
	"encoding/json"
	"testing"
)

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
