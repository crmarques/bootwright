package ceph

import (
	"encoding/base64"
	"encoding/json"

	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const ClientOutput = "client"

func ClientBlockID(client string) string { return reconciliation.BlockIdentity("ceph-client-", client) }

func ClientKeyring(value []byte, client string) ([]byte, bool) {
	if client == "" {
		return nil, false
	}
	entries, err := reconciliation.DecodeEvidence[[]clientCredential](value, 65536, "Ceph client material")
	if err != nil || len(entries) != 1 || canonicaljson.Prove(value, entries, canonicaljson.Bare) != nil {
		return nil, false
	}
	raw, err := reconciliation.DecodeEvidence[[]map[string]json.RawMessage](value, 65536, "Ceph client members")
	if err != nil || len(raw) != 1 || len(raw[0]) != 3 {
		return nil, false
	}
	for _, name := range []string{"entity", "key", "caps"} {
		if _, ok := raw[0][name]; !ok {
			return nil, false
		}
	}
	entry := entries[0]
	key, err := base64.StdEncoding.DecodeString(entry.Key)
	clear(key)
	if err != nil || len(key) == 0 || entry.Entity != "client."+client {
		return nil, false
	}
	keyring := make([]byte, 0, len(entry.Entity)+len(entry.Key)+12)
	keyring = append(keyring, "["+entry.Entity+"]\n\tkey = "...)
	keyring = append(keyring, entry.Key...)
	return append(keyring, '\n'), true
}
