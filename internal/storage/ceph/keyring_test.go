package ceph

import (
	"strings"
	"testing"
)

const keyringKey = "AQBx57l5Y2FuYXJ5LWNsaWVudC1rZXk9PQ=="

func custodiedClient(entity, key string) string {
	return `[{"caps":{"mgr":"profile rbd pool=volumes","mon":"profile rbd","osd":"profile rbd pool=volumes"},"entity":"` + entity + `","key":"` + key + `"}]`
}

// The keyring form is the one ceph auth get-or-create prints and cephadm
// writes for a client: "[client.<name>]\n\tkey = <key>\n" (upstream
// src/auth/KeyRing.cc, KeyRing::print, and src/cephadm/cephadm.py, the
// bootstrap admin keyring, https://github.com/ceph/ceph). Caps stay out: a
// consumer authenticates with the key, and the cluster holds the caps.
func TestClientKeyringRendersTheNativeKeyringForm(t *testing.T) {
	keyring, ok := ClientKeyring([]byte(custodiedClient("client.vm-disks", keyringKey)), "vm-disks")
	if want := "[client.vm-disks]\n\tkey = " + keyringKey + "\n"; !ok || string(keyring) != want {
		t.Fatalf("keyring = %q (%t), want %q", keyring, ok, want)
	}
	for name, value := range map[string]string{
		"another entity":   custodiedClient("client.other", keyringKey),
		"a bare entity":    custodiedClient("vm-disks", keyringKey),
		"two entries":      "[" + strings.Trim(custodiedClient("client.vm-disks", keyringKey), "[]") + "," + strings.Trim(custodiedClient("client.vm-disks", keyringKey), "[]") + "]",
		"no entry":         "[]",
		"non-canonical":    strings.Replace(custodiedClient("client.vm-disks", keyringKey), `"entity":`, `"entity" :`, 1),
		"trailing LF":      custodiedClient("client.vm-disks", keyringKey) + "\n",
		"an extra member":  strings.Replace(custodiedClient("client.vm-disks", keyringKey), `"key":`, `"extra":"x","key":`, 1),
		"a missing member": `[{"entity":"client.vm-disks","key":"` + keyringKey + `"}]`,
		"a bad key":        custodiedClient("client.vm-disks", "not base64!"),
		"an empty key":     custodiedClient("client.vm-disks", ""),
		"a keyring":        "[client.vm-disks]\n\tkey = " + keyringKey + "\n",
		"over the bound":   custodiedClient("client.vm-disks", strings.Repeat("A", 65536)),
	} {
		if keyring, ok := ClientKeyring([]byte(value), "vm-disks"); ok || keyring != nil {
			t.Errorf("%s rendered %q", name, keyring)
		}
	}
	if keyring, ok := ClientKeyring([]byte(custodiedClient("client.", keyringKey)), ""); ok {
		t.Errorf("an empty client name rendered %q", keyring)
	}
}

func TestClientBlockAndOutputNameTheCapturedEntry(t *testing.T) {
	if ClientBlockID("vm-disks") != "ceph-client-vm-disks" || ClientOutput != "client" {
		t.Fatalf("client entry = %s/%s", ClientBlockID("vm-disks"), ClientOutput)
	}
}
