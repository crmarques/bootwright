package localkeyring

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func FuzzLocalStoreRecords(f *testing.F) {
	selector := secretstore.Selector{SelectorVersion: formatVersion, ContextID: "ctx-fixture", Backend: New().Backend(), Generation: "gen-fixture"}
	canonical, err := encodeCanonical(selector, selectorMaximum)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(uint8(0), canonical)
	f.Add(uint8(1), []byte("{}\n"))
	f.Add(uint8(2), []byte("null\n"))
	f.Add(uint8(3), []byte("{\"formatVersion\":1,\"formatVersion\":2}\n"))
	f.Add(uint8(4), []byte("[]\n"))
	f.Add(uint8(5), []byte("{\"keyId\":\"key-fixture\",\"nonce\":\"\",\"ciphertext\":\"\"}\n"))
	f.Add(uint8(6), []byte("{}\n"))
	f.Add(uint8(7), []byte("{}\n"))
	f.Add(uint8(8), []byte("{}\n"))
	f.Fuzz(func(t *testing.T, kind uint8, data []byte) {
		if len(data) > 256<<10 {
			return
		}
		var target any
		switch kind % 9 {
		case 0:
			target = new(secretstore.Selector)
		case 1:
			target = new(indexRecord)
		case 2:
			target = new(envelope)
		case 3:
			target = new(sealLedger)
		case 4:
			target = new(initializationRecord)
		case 5:
			target = new(metadataEnvelope)
		case 6:
			target = new(upgradeRecord)
		case 7:
			target = new(legacySelector)
		case 8:
			target = new(legacyIndex)
		}
		if decodeCanonical(data, indexMaximum, 1<<20, target) == nil {
			encoded, err := encodeCanonical(target, indexMaximum)
			if err != nil || !bytes.Equal(encoded, data) {
				t.Fatal("noncanonical persisted record was accepted")
			}
			if index, ok := target.(*indexRecord); ok {
				_ = validateIndex(*index, index.Selector)
			}
		}
		if kind%9 == 2 {
			plaintext, _ := openEnvelope(data, make([]byte, 32), []byte("synthetic-authentication-domain"), "part", "key-fixture", "blob-fixture", partMaximum)
			clear(plaintext)
		}
	})
}

func FuzzCanonicalStoreSizePreflight(f *testing.F) {
	f.Add("first", "second", uint16(10))
	f.Add("<>&\x00\xff\u2028", "", uint16(1))
	f.Fuzz(func(t *testing.T, first, second string, budget uint16) {
		if len(first)+len(second) > 4096 {
			return
		}
		for _, value := range []any{
			[]string{first, second},
			envelope{Purpose: first, Ciphertext: second},
			secrets.Declaration{Origin: first, Generation: secrets.Generation{DNSNames: []string{first, second}}},
			indexRecord{ActiveKey: first, Legacy: budget%2 == 0, Keys: []storedKey{{ID: second}}},
		} {
			encoded, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			want := len(encoded) + 1
			for _, maximum := range []int{int(budget), want, want - 1} {
				actual, err := canonicalEncodedSize(value, maximum)
				if want > maximum {
					if err == nil {
						t.Fatal("oversized record passed encoding preflight")
					}
				} else if err != nil || actual != want {
					t.Fatal("encoding preflight did not match canonical JSON size")
				}
			}
		}
	})
}
