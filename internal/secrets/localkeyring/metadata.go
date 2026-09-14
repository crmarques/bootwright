package localkeyring

import (
	"context"
	"io"
	"slices"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

type metadataEnvelope struct {
	KeyID      string `json:"keyId"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func keyPath(id string) string    { return "keys/" + id + ".key" }
func ledgerPath(id string) string { return "keys/" + id + ".usage.json" }
func partPath(id string) string   { return "parts/" + id + ".enc" }

func metadataEncodedSize(plaintext int, selector secretstore.Selector, keyID string) (int, error) {
	if plaintext < 0 || plaintext > indexMaximum-gcmTagSize {
		return 0, secretstore.Failure("store.limit", "secret metadata exceeds its encoding limit")
	}
	payload, err := encodeCanonical(metadataEnvelope{KeyID: keyID, Nonce: "AAAAAAAAAAAAAAAA"}, indexMaximum)
	if err != nil {
		return 0, err
	}
	data, err := secretstore.EncodeRecord(selector, payload)
	if err != nil {
		return 0, err
	}
	size, valid := boundedSize(len(data), rawBase64.EncodedLen(plaintext+gcmTagSize), secretstore.RecordMaximum)
	if !valid {
		return 0, secretstore.Failure("store.limit", "secret metadata exceeds its encoding limit")
	}
	return size, nil
}

func sealMetadata(key, plaintext []byte, selector secretstore.Selector, keyID string, random io.Reader) ([]byte, error) {
	if _, err := metadataEncodedSize(len(plaintext), selector, keyID); err != nil {
		return nil, err
	}
	sealed, err := seal(key, plaintext, indexAAD(selector.Context, selector, keyID), "index", keyID, selector.Generation, random, indexMaximum)
	if err != nil {
		return nil, err
	}
	defer clear(sealed)
	var wrapped envelope
	if err := decodeCanonical(sealed, indexMaximum, 32, &wrapped); err != nil {
		return nil, err
	}
	payload, err := encodeCanonical(metadataEnvelope{KeyID: keyID, Nonce: wrapped.Nonce, Ciphertext: wrapped.Ciphertext}, indexMaximum)
	if err != nil {
		return nil, err
	}
	defer clear(payload)
	return secretstore.EncodeRecord(selector, payload)
}

func openMetadata(record secretstore.Record, key []byte) ([]byte, error) {
	var wrapped metadataEnvelope
	if decodeMetadataPayload(record.Payload, &wrapped) != nil || !validID(wrapped.KeyID, "key-") {
		return nil, secretstore.Failure("store.corrupt", "encrypted secret metadata is malformed")
	}
	data, err := encodeCanonical(envelope{FormatVersion: formatVersion, Algorithm: algorithm, Purpose: "index", KeyID: wrapped.KeyID, BlobID: record.Generation, Nonce: wrapped.Nonce, Ciphertext: wrapped.Ciphertext}, indexMaximum)
	if err != nil {
		return nil, err
	}
	defer clear(data)
	return openEnvelope(data, key, indexAAD(record.Context, record.Selector, wrapped.KeyID), "index", wrapped.KeyID, record.Generation, indexMaximum)
}

func cleanupFailure(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	return secretstore.Failure("store.conflict", "secret metadata committed; cleanup is incomplete; retry secret encryption init")
}

func decodeMetadataPayload(payload []byte, wrapped *metadataEnvelope) error {
	if len(payload) >= indexMaximum {
		return secretstore.Failure("store.limit", "secret metadata exceeds its encoding limit")
	}
	data := append(slices.Clone(payload), '\n')
	defer clear(data)
	return decodeCanonical(data, indexMaximum, 16, wrapped)
}
