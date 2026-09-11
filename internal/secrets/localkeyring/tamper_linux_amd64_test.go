//go:build linux && amd64

package localkeyring

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

func TestEveryAdditionalDataFieldIsAuthenticated(t *testing.T) {
	selection := New().Backend()
	index := indexAdditionalData{Domain: "index", FormatVersion: 1, Algorithm: algorithm, ContextID: "context-example", Selection: selection, Generation: "generation", KeyID: "key", BlobID: "index"}
	part := partAdditionalData{Domain: "part", FormatVersion: 1, Algorithm: algorithm, ContextID: "context-example", Selection: selection, Generation: "generation", KeyID: "key", BlobID: "blob", DeclarationFingerprint: "nonsecret-fingerprint", Name: "credential", Type: "opaque", Source: "contextStore", Version: "version", Part: "value"}
	for _, record := range []any{index, part} {
		t.Run(reflect.TypeOf(record).Name(), func(t *testing.T) {
			aad, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			key := bytes.Repeat([]byte{3}, 32)
			sealed, err := seal(key, []byte("synthetic-authentication-canary"), aad, "part", "key", "blob", bytes.NewReader(make([]byte, 12)), partMaximum)
			if err != nil {
				t.Fatal(err)
			}
			valueRoot := reflect.New(reflect.TypeOf(record)).Elem()
			valueRoot.Set(reflect.ValueOf(record))
			var mutateFields func(reflect.Value, string)
			mutateFields = func(value reflect.Value, prefix string) {
				for n := range value.NumField() {
					field := value.Field(n)
					name := prefix + value.Type().Field(n).Name
					if field.Kind() == reflect.Struct {
						mutateFields(field, name+"/")
						continue
					}
					before := reflect.New(field.Type()).Elem()
					before.Set(field)
					switch field.Kind() {
					case reflect.String:
						field.SetString(field.String() + "-changed")
					case reflect.Int:
						field.SetInt(field.Int() + 1)
					default:
						t.Fatalf("uncovered AAD field %s", name)
					}
					t.Run(name, func(t *testing.T) {
						modified, err := json.Marshal(valueRoot.Interface())
						if err != nil {
							t.Fatal(err)
						}
						plaintext, err := openEnvelope(sealed, key, modified, "part", "key", "blob", partMaximum)
						defer clear(plaintext)
						if err == nil || len(plaintext) != 0 || strings.Contains(err.Error(), "canary") {
							t.Fatal("modified AAD was accepted or disclosed material")
						}
					})
					field.Set(before)
				}
			}
			mutateFields(valueRoot, "")
		})
	}
}

func TestEncryptedEnvelopeRejectsEveryChangedFieldAndKey(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	aad := []byte("synthetic immutable metadata")
	sealed, err := seal(key, []byte("synthetic-envelope-canary"), aad, "part", "key", "blob", bytes.NewReader(make([]byte, 12)), partMaximum)
	if err != nil {
		t.Fatal(err)
	}
	var original envelope
	if err := decodeCanonical(sealed, partMaximum, 32, &original); err != nil {
		t.Fatal(err)
	}
	changes := map[string]func(*envelope){
		"format":       func(r *envelope) { r.FormatVersion++ },
		"algorithm":    func(r *envelope) { r.Algorithm = "unsupported" },
		"purpose":      func(r *envelope) { r.Purpose = "index" },
		"key-id":       func(r *envelope) { r.KeyID += "-other" },
		"blob-id":      func(r *envelope) { r.BlobID += "-other" },
		"nonce-length": func(r *envelope) { r.Nonce = rawBase64.EncodeToString(make([]byte, 13)) },
		"nonce":        func(r *envelope) { r.Nonce = rawBase64.EncodeToString(bytes.Repeat([]byte{1}, 12)) },
		"ciphertext": func(r *envelope) {
			value, _ := rawBase64.DecodeString(r.Ciphertext)
			value[0] ^= 1
			r.Ciphertext = rawBase64.EncodeToString(value)
		},
		"tag": func(r *envelope) {
			value, _ := rawBase64.DecodeString(r.Ciphertext)
			value[len(value)-1] ^= 1
			r.Ciphertext = rawBase64.EncodeToString(value)
		},
		"short-tag":      func(r *envelope) { r.Ciphertext = rawBase64.EncodeToString(make([]byte, 15)) },
		"base64-padding": func(r *envelope) { r.Ciphertext += "=" },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			record := original
			change(&record)
			data, err := encodeCanonical(record, partMaximum)
			if err != nil {
				t.Fatal(err)
			}
			plaintext, err := openEnvelope(data, key, aad, "part", "key", "blob", partMaximum)
			defer clear(plaintext)
			if err == nil || len(plaintext) != 0 || strings.Contains(err.Error(), "canary") {
				t.Fatal("changed envelope was accepted or disclosed material")
			}
		})
	}
	for _, changedKey := range [][]byte{nil, make([]byte, 31), bytes.Repeat([]byte{8}, 32), make([]byte, 33)} {
		plaintext, err := openEnvelope(sealed, changedKey, aad, "part", "key", "blob", partMaximum)
		clear(plaintext)
		if err == nil || len(plaintext) != 0 {
			t.Fatal("invalid key was accepted")
		}
	}
}

func TestPublishedStoreCorruptionNeverFallsBackOrWrites(t *testing.T) {
	for _, target := range []string{"selector", "index", "key", "missing-key", "ledger", "part", "part-length"} {
		t.Run(target, func(t *testing.T) {
			h := newIntegrationStore(t)
			material := opaque("synthetic-persisted-canary")
			defer material.Clear()
			var version secretstore.Version
			if err := h.mutate(func(session secretstore.StoreSession) error {
				versions, err := session.PutBatch(context.Background(), []secretstore.Put{{Declaration: declaration("credential", "opaque", "contextStore"), Material: material}})
				if err == nil {
					version = versions[0]
				}
				return err
			}); err != nil {
				t.Fatal(err)
			}
			var selected secretstore.Selector
			var current indexRecord
			if err := h.workspace.ReadSecrets(context.Background(), h.context, func(area secretstore.Area) error {
				selected = readTestSelector(t, area)
				opened, err := h.implementation.Open(context.Background(), h.context, area, selected, nil)
				if err != nil {
					return err
				}
				defer opened.Close()
				current = opened.(*session).index
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(h.root, "contexts", h.context.Name, "secrets")
			path := filepath.Join(root, selectorPath)
			switch target {
			case "index":
				path = filepath.Join(root, selectorPath)
			case "key", "missing-key":
				path = filepath.Join(root, keyPath(current.ActiveKey))
			case "ledger":
				path = filepath.Join(root, ledgerPath(current.ActiveKey))
			case "part", "part-length":
				path = filepath.Join(root, partPath(current.Versions[0].Parts[0].BlobID))
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			switch target {
			case "missing-key":
				err = os.Remove(path)
			case "part-length":
				err = os.WriteFile(path, nil, 0600)
			default:
				data[len(data)/2] ^= 1
				err = os.WriteFile(path, data, 0600)
			}
			if err != nil {
				t.Fatal(err)
			}
			access := secretstore.NewAccess(h.workspace, secretstore.NewCatalog(h.implementation), nil)
			for _, mutation := range []bool{false, true} {
				operation := access.View
				var err error
				callback := func(session secretstore.StoreSession, _ secretstore.Selection) error {
					value, err := session.Read(context.Background(), version.ID)
					value.Clear()
					return err
				}
				before := artifactTree(t, root)
				if mutation {
					err = access.Mutate(context.Background(), h.context, callback)
				} else {
					err = operation(context.Background(), h.context, true, callback)
				}
				if err == nil || strings.Contains(err.Error(), "canary") {
					t.Fatal("corruption was accepted or disclosed material")
				}
				if !reflect.DeepEqual(before, artifactTree(t, root)) {
					t.Fatal("corruption handling changed the persisted store")
				}
			}
		})
	}
}

func artifactTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err == nil {
			result[path] = data
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
