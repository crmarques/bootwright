package ceph

import (
	"encoding/base64"
	"encoding/json"
	"maps"
	"strings"

	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const (
	DashboardOutput   = "dashboard"
	DashboardUser     = "admin"
	dashboardLength   = 32
	dashboardAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_."
)

func DashboardBlock(cluster string) string { return "ceph-cluster-" + cluster }

type clientCredential struct {
	Caps   map[string]string `json:"caps"`
	Entity string            `json:"entity"`
	Key    string            `json:"key"`
}

// validateProduced confines a native auth result to the one least-privilege
// principal this request owns. The value is never returned as diagnostics.
func validateProduced(produced []lifecycle.Produced, r Request, present bool) error {
	if present && r.Action == "cluster" {
		return validateDashboard(produced)
	}
	if !present || r.Action != "client" {
		if len(produced) != 0 {
			return evidenceFailure()
		}
		return nil
	}
	if r.Client == nil || len(produced) != 1 || produced[0].Name != "client" || len(produced[0].Material.Parts()) != 1 {
		return evidenceFailure()
	}
	value, ok := produced[0].Material.Part(secrets.ValuePart)
	if !ok {
		return evidenceFailure()
	}
	defer clear(value)
	entries, err := reconciliation.DecodeEvidence[[]clientCredential](value, 65536, "Ceph client material")
	if err != nil || len(entries) != 1 || canonicaljson.Prove(value, entries, canonicaljson.Bare) != nil {
		return evidenceFailure()
	}
	raw, err := reconciliation.DecodeEvidence[[]map[string]json.RawMessage](value, 65536, "Ceph client members")
	if err != nil || len(raw[0]) != 3 {
		return evidenceFailure()
	}
	for _, name := range []string{"entity", "key", "caps"} {
		if _, ok := raw[0][name]; !ok {
			return evidenceFailure()
		}
	}
	profile := "rbd"
	if r.Client.Access == "read-only" {
		profile = "rbd-read-only"
	} else if r.Client.Access != "read-write" {
		return evidenceFailure()
	}
	caps := map[string]string{"mon": "profile rbd", "mgr": "profile " + profile + " pool=" + r.Client.Pool, "osd": "profile " + profile + " pool=" + r.Client.Pool}
	key, err := base64.StdEncoding.DecodeString(entries[0].Key)
	defer clear(key)
	if err != nil || len(key) == 0 || entries[0].Entity != "client."+r.Client.Name || !maps.Equal(entries[0].Caps, caps) {
		return evidenceFailure()
	}
	return nil
}

func validateDashboard(produced []lifecycle.Produced) error {
	if len(produced) != 1 || produced[0].Name != DashboardOutput || len(produced[0].Material.Parts()) != 1 {
		return evidenceFailure()
	}
	value, ok := produced[0].Material.Part(secrets.ValuePart)
	if !ok {
		return evidenceFailure()
	}
	defer clear(value)
	if len(value) != dashboardLength {
		return evidenceFailure()
	}
	for _, b := range value {
		if !strings.ContainsRune(dashboardAlphabet, rune(b)) {
			return evidenceFailure()
		}
	}
	return nil
}
