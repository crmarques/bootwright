package ceph

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

func smbShareRequest(t *testing.T) SMBRequest {
	t.Helper()
	catalog := smbGraph(t)
	return builtSMB(t, catalog, sambaBase(t, catalog), "smb-docs")
}

func TestSMBEvidenceIsClosed(t *testing.T) {
	r := smbShareRequest(t)
	e := smbExecution(t, r, "pw")
	owner, fsid, digest := "apply-original", FSIDFor("apply-original", "example"), e.Block.RequestDigest
	evaluate := func(data []byte, removal bool) (reconciliation.EffectState, error) {
		return EvaluateSMBEvidence(data, r, owner, fsid, digest, removal)
	}
	ready := smbReady(t, e, r, nil)
	if effect, err := evaluate(ready, false); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("ready evidence is %s (%v)", effect, err)
	}
	if effect, err := evaluate(ready, true); err != nil || effect != reconciliation.EffectNoEffect {
		t.Fatalf("ready evidence of a removal is %s (%v)", effect, err)
	}
	partial := smbReady(t, e, r, func(s *SMBEvidence) { s.Ready, s.Partial = false, true })
	if effect, err := evaluate(partial, false); err != nil || effect != reconciliation.EffectPartial {
		t.Fatalf("partial evidence is %s (%v)", effect, err)
	}
	absent := smbReady(t, e, r, func(s *SMBEvidence) { s.Ready, s.Exists, s.Resource, s.Daemons = false, false, "", []SMBDaemon{} })
	if effect, err := evaluate(absent, false); err != nil || effect != reconciliation.EffectNoEffect {
		t.Fatalf("absent evidence is %s (%v)", effect, err)
	}
	removed := smbReady(t, e, r, func(s *SMBEvidence) {
		s.Ready, s.Exists, s.Removed, s.Resource, s.Daemons = false, false, true, "", []SMBDaemon{}
	})
	if effect, err := evaluate(removed, true); err != nil || effect != reconciliation.EffectCompleted {
		t.Fatalf("removed evidence is %s (%v)", effect, err)
	}
	if effect, err := evaluate(absent, true); err == nil || effect != reconciliation.EffectUnknown {
		t.Fatalf("absent evidence without removed proves a removal: %s", effect)
	}
	tampered := map[string]string{
		"an extra member":    strings.Replace(string(ready), `{"daemons"`, `{"absent":false,"daemons"`, 1),
		"a missing member":   strings.Replace(string(ready), `"partial":false,`, "", 1),
		"a null member":      strings.Replace(string(ready), `"resource":"ceph.smb.share.files.docs"`, `"resource":null`, 1),
		"a reordered member": strings.Replace(string(ready), `"owned":true,"owner":"apply-original"`, `"owner":"apply-original","owned":true`, 1),
		"a daemon member":    strings.Replace(string(ready), `"host":"node-1",`, `"host":"node-1","image":"other",`, 1),
	}
	for name, data := range tampered {
		if data == string(ready) {
			t.Fatalf("%s: the tampering did not change the evidence", name)
		}
		if effect, err := evaluate([]byte(data), false); err == nil || effect != reconciliation.EffectUnknown {
			t.Fatalf("%s is %s", name, effect)
		}
	}
	for name, change := range map[string]func(*SMBEvidence){
		"another service":   func(s *SMBEvidence) { s.Daemons[0].Service = "smb.other" },
		"another host":      func(s *SMBEvidence) { s.Daemons[0].Host = "foreign" },
		"a foreign name":    func(s *SMBEvidence) { s.Daemons[0].Name = "nfs.files.0" },
		"no daemon":         func(s *SMBEvidence) { s.Daemons = []SMBDaemon{} },
		"a wrong resource":  func(s *SMBEvidence) { s.Resource = "ceph.smb.share.other.docs" },
		"another owner":     func(s *SMBEvidence) { s.Owner = "apply-other" },
		"another fsid":      func(s *SMBEvidence) { s.FSID = FSIDFor("apply-other", "example") },
		"another request":   func(s *SMBEvidence) { s.Request = strings.Repeat("0", 64) },
		"another version":   func(s *SMBEvidence) { s.Version = "ceph-smb-evidence-v0" },
		"foreign ownership": func(s *SMBEvidence) { s.Owned = false },
		"ready and removed": func(s *SMBEvidence) { s.Removed = true },
	} {
		if effect, err := evaluate(smbReady(t, e, r, change), false); err == nil || effect != reconciliation.EffectUnknown {
			t.Fatalf("%s is %s", name, effect)
		}
	}
}

func TestSMBEvidenceNamesEachResourceType(t *testing.T) {
	catalog := smbCatalog(t, subvolumeOf(), smbUsers(), smbJoin(), smbFiles(requestMap("label", "mon", "count", 2)), smbDocs())
	base := sambaBase(t, catalog)
	for name, want := range map[string]string{"smb-users": "ceph.smb.usersgroups.users", "smb-join": "ceph.smb.join.auth.join", "smb-files": "ceph.smb.cluster.files", "smb-docs": "ceph.smb.share.files.docs"} {
		r := builtSMB(t, catalog, base, name)
		if got := smbResourceName(r); got != want {
			t.Fatalf("%s names %s, want %s", name, got, want)
		}
		if r.ResourceType == "ceph.smb.share" {
			continue
		}
		e := smbExecution(t, r, "pw")
		daemon := smbReady(t, e, r, func(s *SMBEvidence) {
			s.Daemons = []SMBDaemon{{Host: "node-1", Name: "smb.files.0", Service: "smb.files"}}
		})
		if effect, err := EvaluateSMBEvidence(daemon, r, "apply-original", FSIDFor("apply-original", "example"), e.Block.RequestDigest, false); err == nil || effect != reconciliation.EffectUnknown {
			t.Fatalf("%s evidence with a daemon is %s", name, effect)
		}
	}
	if _, err := json.Marshal(SMBEvidence{}); err != nil {
		t.Fatal(err)
	}
}
