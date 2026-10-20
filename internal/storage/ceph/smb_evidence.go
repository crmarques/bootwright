package ceph

import (
	"encoding/json"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const maxSMBEvidence = 16384

type SMBDaemon struct {
	Host    string `json:"host"`
	Name    string `json:"name"`
	Service string `json:"service"`
}

type SMBEvidence struct {
	Daemons  []SMBDaemon `json:"daemons"`
	Exists   bool        `json:"exists"`
	FSID     string      `json:"fsid"`
	Owned    bool        `json:"owned"`
	Owner    string      `json:"owner"`
	Partial  bool        `json:"partial"`
	Ready    bool        `json:"ready"`
	Removed  bool        `json:"removed"`
	Request  string      `json:"request"`
	Resource string      `json:"resource"`
	Version  string      `json:"version"`
}

func smbEvidenceFailure() error {
	return failure("lifecycle.unknown", "Ceph SMB evidence does not prove the frozen ownership and resource scope", "")
}

func decodeSMBEvidence(data []byte) (SMBEvidence, bool) {
	e, err := reconciliation.DecodeEvidence[SMBEvidence](data, maxSMBEvidence, "Ceph SMB")
	if err != nil {
		return SMBEvidence{}, false
	}
	fields, err := reconciliation.DecodeEvidence[map[string]json.RawMessage](data, maxSMBEvidence, "Ceph SMB members")
	if err != nil || !closedMembers(fields, "daemons", "exists", "fsid", "owned", "owner", "partial", "ready", "removed", "request", "resource", "version") || canonicaljson.ProveObject(data) != nil {
		return SMBEvidence{}, false
	}
	daemons, err := reconciliation.DecodeEvidence[[]map[string]json.RawMessage](fields["daemons"], maxSMBEvidence, "Ceph SMB daemon members")
	if err != nil {
		return SMBEvidence{}, false
	}
	for _, daemon := range daemons {
		if !closedMembers(daemon, "host", "name", "service") {
			return SMBEvidence{}, false
		}
	}
	return e, true
}

func smbResourceName(r SMBRequest) string {
	switch r.ResourceType {
	case api.SMBShare:
		if r.Service == nil {
			return ""
		}
		return r.ResourceType + "." + strings.TrimPrefix(r.Service.Name, "smb.") + "." + r.ID
	case api.SMBCluster, api.SMBJoinAuth, api.SMBUsersGroups:
		return r.ResourceType + "." + r.ID
	}
	return ""
}

func smbDaemonsMatch(e SMBEvidence, r SMBRequest) bool {
	if r.ResourceType != api.SMBShare || r.Service == nil {
		return len(e.Daemons) == 0
	}
	for _, d := range e.Daemons {
		if !unitDaemonName.MatchString(d.Name) || !strings.HasPrefix(d.Name, "smb.") || d.Service != r.Service.Name || !slices.Contains(r.Service.Hosts, d.Host) {
			return false
		}
	}
	return !e.Ready || len(e.Daemons) != 0
}

func EvaluateSMBEvidence(data []byte, r SMBRequest, owner, fsid, digest string, removal bool) (reconciliation.EffectState, error) {
	e, ok := decodeSMBEvidence(data)
	if !ok || e.Version != SMBEvidenceVersion || digest == "" || e.Request != digest || owner == "" || e.Owner != owner || fsid == "" || e.FSID != fsid {
		return reconciliation.EffectUnknown, smbEvidenceFailure()
	}
	if e.Ready && (!e.Owned || !e.Exists || e.Partial || e.Removed) || e.Removed && (!e.Owned || e.Exists || e.Ready || e.Partial) || e.Partial && (!e.Owned || !e.Exists || e.Ready || e.Removed) {
		return reconciliation.EffectUnknown, smbEvidenceFailure()
	}
	if !e.Exists && !e.Ready && !e.Partial {
		if e.Resource != "" || len(e.Daemons) != 0 || removal && !e.Removed {
			return reconciliation.EffectUnknown, smbEvidenceFailure()
		}
		if removal {
			return reconciliation.EffectCompleted, nil
		}
		return reconciliation.EffectNoEffect, nil
	}
	if !e.Owned || !smbDaemonsMatch(e, r) {
		return reconciliation.EffectUnknown, smbEvidenceFailure()
	}
	if e.Ready && e.Resource == smbResourceName(r) {
		if removal {
			return reconciliation.EffectNoEffect, nil
		}
		return reconciliation.EffectCompleted, nil
	}
	if e.Partial && (e.Resource == "" || e.Resource == smbResourceName(r)) {
		return reconciliation.EffectPartial, nil
	}
	return reconciliation.EffectUnknown, smbEvidenceFailure()
}
