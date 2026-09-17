package trust

import (
	"strings"
	"testing"
)

func record(t *testing.T, machine, address string, seed byte) Record {
	t.Helper()
	host := key(t, seed)
	return Record{
		Machine: machine, Address: address, Port: 22,
		KeyType: host.Type, PublicKey: host.PublicKey, Fingerprint: host.Fingerprint(),
		Source: SourceEnrollment, Recorded: "2026-09-16T00:00:00Z",
	}
}

func TestAnAbsentTrustStoreReadsAsEmptyRatherThanDamaged(t *testing.T) {
	for _, data := range [][]byte{nil, {}, []byte("  \n ")} {
		store, err := Decode(data)
		if err != nil {
			t.Fatalf("%q: %v", data, err)
		}
		if store.FormatVersion != FormatVersion || len(store.Hosts) != 0 {
			t.Fatalf("%q decoded to %+v", data, store)
		}
	}
}

func TestTrustRecordsRoundTripInMachineOrder(t *testing.T) {
	store := Store{FormatVersion: FormatVersion}
	store.Upsert(record(t, "node-b", "192.0.2.11", 2))
	store.Upsert(record(t, "node-a", "192.0.2.10", 1))
	data, err := store.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Hosts) != 2 || decoded.Hosts[0].Machine != "node-a" || decoded.Hosts[1].Machine != "node-b" {
		t.Fatalf("decoded = %+v", decoded.Hosts)
	}
	again, err := decoded.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(data) {
		t.Fatalf("re-encoded differs:\n%s\n%s", data, again)
	}
}

func TestUpsertReplacesOneMachineWithoutDisturbingAnother(t *testing.T) {
	store := Store{FormatVersion: FormatVersion}
	store.Upsert(record(t, "node-a", "192.0.2.10", 1))
	store.Upsert(record(t, "node-b", "192.0.2.11", 2))
	rotated := record(t, "node-a", "192.0.2.10", 3)
	store.Upsert(rotated)
	if len(store.Hosts) != 2 {
		t.Fatalf("hosts = %+v", store.Hosts)
	}
	found, ok := store.Find("node-a")
	if !ok || found.PublicKey != rotated.PublicKey {
		t.Fatalf("node-a = %+v", found)
	}
	if _, ok := store.Find("absent"); ok {
		t.Fatal("an unrecorded Machine was found")
	}
}

// OpenSSH accepts any entry matching a host, so two records pinning one address
// to different keys would let a peer present either and still pass.
func TestOneAddressPinsExactlyOneKey(t *testing.T) {
	store := Store{FormatVersion: FormatVersion}
	store.Upsert(record(t, "node-a", "192.0.2.10", 1))
	store.Upsert(record(t, "node-b", "192.0.2.10", 2))
	err := store.Validate()
	if err == nil {
		t.Fatal("divergent keys for one address were accepted")
	}
	if got := code(t, err); got != "trust.identity" {
		t.Fatalf("code = %q", got)
	}
	if _, encodeErr := store.Encode(); encodeErr == nil {
		t.Fatal("encoding published a store no session could read")
	}
	shared := record(t, "node-b", "192.0.2.10", 1)
	store.Upsert(shared)
	if err := store.Validate(); err != nil {
		t.Fatalf("two Machines at one address with the same key: %v", err)
	}
}

func TestATrustStoreRefusesContentNoSessionCouldRead(t *testing.T) {
	valid := record(t, "node-a", "192.0.2.10", 1)
	for _, test := range []struct{ name, data string }{
		{"unknown field", `{"formatVersion":1,"hosts":[],"extra":true}`},
		{"another version", `{"formatVersion":2,"hosts":[]}`},
		{"trailing content", `{"formatVersion":1,"hosts":[]}{}`},
		{"malformed", `{"formatVersion":1,`},
		{"unqualified key type", `{"formatVersion":1,"hosts":[{"machine":"a","address":"192.0.2.10","port":22,"keyType":"ssh-dss","publicKey":"` + valid.PublicKey + `"}]}`},
		{"no machine", `{"formatVersion":1,"hosts":[{"machine":"","address":"192.0.2.10","port":22,"keyType":"` + valid.KeyType + `","publicKey":"` + valid.PublicKey + `"}]}`},
		{"duplicate machine", `{"formatVersion":1,"hosts":[{"machine":"a","address":"192.0.2.10","port":22,"keyType":"` + valid.KeyType + `","publicKey":"` + valid.PublicKey + `"},{"machine":"a","address":"192.0.2.11","port":22,"keyType":"` + valid.KeyType + `","publicKey":"` + valid.PublicKey + `"}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if store, err := Decode([]byte(test.data)); err == nil {
				t.Fatalf("accepted %s as %+v", test.data, store)
			}
		})
	}
}

func TestATrustStoreIsBoundedInRecordsAndBytes(t *testing.T) {
	store := Store{FormatVersion: FormatVersion}
	base := record(t, "node", "192.0.2.10", 1)
	for i := range MaxHosts + 1 {
		entry := base
		entry.Machine = "node-" + strings.Repeat("a", i%3) + string(rune('a'+i%26)) + strings.Repeat("b", i/26)
		entry.Address = "192.0.2." + string(rune('0'+i%10))
		store.Hosts = append(store.Hosts, entry)
	}
	if err := store.Validate(); err == nil {
		t.Fatal("an unbounded store was accepted")
	}
}

func TestARecordCarriesTheKeyASessionPins(t *testing.T) {
	entry := record(t, "node-a", "192.0.2.10", 1)
	host := entry.HostKey()
	if !host.Present() || host.Fingerprint() != entry.Fingerprint {
		t.Fatalf("record key = %+v", host)
	}
}
