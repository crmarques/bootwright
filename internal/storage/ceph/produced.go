package ceph

import (
	"encoding/base64"
	"encoding/json"
	"maps"
	"slices"
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

func DashboardBlock(cluster string) string { return ClusterBlockID(cluster) }

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
	if present && r.Action == "object-user" {
		return validateObjectUser(produced, r)
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
	caps, ok := clientCaps(*r.Client)
	if !ok || !strictlyAscending(r.Client.DataPools) || slices.Contains(r.Client.DataPools, r.Client.Pool) {
		return evidenceFailure()
	}
	key, err := base64.StdEncoding.DecodeString(entries[0].Key)
	defer clear(key)
	if err != nil || len(key) == 0 || entries[0].Entity != "client."+r.Client.Name || !maps.Equal(entries[0].Caps, caps) {
		return evidenceFailure()
	}
	return nil
}

func clientCaps(c Client) (map[string]string, bool) {
	if c.Pool != "" && c.Filesystem != "" || c.Group != "" && c.Subvolume == "" || c.Subvolume != "" && c.Filesystem == "" || (c.Subvolume != "") != (c.SubvolumePath != "") {
		return nil, false
	}
	if c.Filesystem != "" {
		return cephFSClientCaps(c)
	}
	if c.Pool == "" {
		return nil, false
	}
	profile := "rbd"
	if c.Access == "readOnly" {
		profile = "rbd-read-only"
	} else if c.Access != "readWrite" {
		return nil, false
	}
	grants := []string{"profile " + profile + " pool=" + c.Pool}
	for _, pool := range c.DataPools {
		grants = append(grants, "profile "+profile+" pool="+pool)
	}
	return map[string]string{"mon": "profile rbd", "mgr": "profile " + profile + " pool=" + c.Pool, "osd": strings.Join(grants, ", ")}, true
}

func cephFSClientCaps(c Client) (map[string]string, bool) {
	grant := map[string]string{"readOnly": "r", "readWrite": "rw"}[c.Access]
	if grant == "" || len(c.DataPools) != 0 {
		return nil, false
	}
	mds := "allow " + grant + " fsname=" + c.Filesystem
	if c.Subvolume != "" {
		mds += " path=" + c.SubvolumePath
	}
	return map[string]string{"mds": mds, "mon": "allow r fsname=" + c.Filesystem, "osd": "allow " + grant + " tag cephfs data=" + c.Filesystem}, true
}

func strictlyAscending(values []string) bool {
	for i := 1; i < len(values); i++ {
		if values[i-1] >= values[i] {
			return false
		}
	}
	return true
}

type objectUserCredential struct {
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
	UID       string `json:"uid"`
}

func validateObjectUser(produced []lifecycle.Produced, r Request) error {
	if r.ObjectUser == nil || len(produced) != 1 || produced[0].Name != ObjectUserOutput || len(produced[0].Material.Parts()) != 1 {
		return evidenceFailure()
	}
	value, ok := produced[0].Material.Part(secrets.ValuePart)
	if !ok {
		return evidenceFailure()
	}
	defer clear(value)
	entry, err := reconciliation.DecodeEvidence[objectUserCredential](value, 4096, "Ceph object user material")
	if err != nil || canonicaljson.Prove(value, entry, canonicaljson.Bare) != nil {
		return evidenceFailure()
	}
	raw, err := reconciliation.DecodeEvidence[map[string]json.RawMessage](value, 4096, "Ceph object user members")
	if err != nil || !closedMembers(raw, "accessKey", "secretKey", "uid") {
		return evidenceFailure()
	}
	if entry.UID != r.ObjectUser.Name || !visibleKey(entry.AccessKey) || !visibleKey(entry.SecretKey) {
		return evidenceFailure()
	}
	return nil
}

func visibleKey(key string) bool {
	if len(key) == 0 || len(key) > 128 {
		return false
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x21 || key[i] > 0x7e || key[i] == '&' || key[i] == '<' || key[i] == '>' {
			return false
		}
	}
	return true
}

func ObjectUserCredentials(value []byte, user string) (secrets.Material, bool) {
	entry, err := reconciliation.DecodeEvidence[objectUserCredential](value, 4096, "Ceph object user material")
	if err != nil || canonicaljson.Prove(value, entry, canonicaljson.Bare) != nil || entry.UID != user || !visibleKey(entry.AccessKey) || !visibleKey(entry.SecretKey) {
		return secrets.Material{}, false
	}
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte(entry.AccessKey), secrets.PasswordPart: []byte(entry.SecretKey)}), true
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
