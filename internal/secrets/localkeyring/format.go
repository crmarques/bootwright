package localkeyring

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

const (
	formatVersion    = 4
	algorithm        = "AES-256-GCM"
	selectorPath     = secretstore.RecordPath
	selectorMaximum  = 64 << 10
	indexMaximum     = 8 << 20
	ledgerMaximum    = 64 << 10
	partMaximum      = 2 << 20
	maxBindings      = 4096
	maxPhysicalItems = 32768
	maxIndexItems    = indexMaximum
	maxSeals         = uint64(1 << 20)
	gcmNonceSize     = 12
	gcmTagSize       = 16
)

var rawBase64 = base64.RawURLEncoding.Strict()

type envelope struct {
	FormatVersion int    `json:"formatVersion"`
	Algorithm     string `json:"algorithm"`
	Purpose       string `json:"purpose"`
	KeyID         string `json:"keyId"`
	BlobID        string `json:"blobId"`
	Nonce         string `json:"nonce"`
	Ciphertext    string `json:"ciphertext"`
}

type indexRecord struct {
	FormatVersion int                    `json:"-"`
	Algorithm     string                 `json:"-"`
	Selector      secretstore.Selector   `json:"-"`
	ActiveKey     string                 `json:"activeKey"`
	Keys          []storedKey            `json:"keys"`
	Versions      []storedVersion        `json:"versions"`
	Current       []secretstore.Current  `json:"current"`
	Bindings      []secretstore.Binding  `json:"bindings"`
	Produced      []secretstore.Produced `json:"produced"`
}

type storedKey struct {
	ID    string `json:"id"`
	Seals uint64 `json:"seals"`
}

type storedVersion struct {
	ID          string                     `json:"id"`
	Sequence    int                        `json:"sequence"`
	Declaration secrets.VersionDeclaration `json:"declaration"`
	Parts       []storedPart               `json:"parts"`
}

type storedPart struct {
	Part       secrets.Part `json:"part"`
	BlobID     string       `json:"blobId"`
	KeyID      string       `json:"keyId"`
	Generation string       `json:"generation"`
	Size       int          `json:"size"`
}

type sealLedger struct {
	FormatVersion int    `json:"formatVersion"`
	KeyID         string `json:"keyId"`
	Seals         uint64 `json:"seals"`
	MAC           string `json:"mac"`
}

type ledgerAuthentication struct {
	Domain        string `json:"domain"`
	FormatVersion int    `json:"formatVersion"`
	Algorithm     string `json:"algorithm"`
	Context       string `json:"context"`
	Selection     string `json:"backend"`
	KeyID         string `json:"keyId"`
	Seals         uint64 `json:"seals"`
}

type initializationRecord struct {
	FormatVersion int                     `json:"formatVersion"`
	Context       string                  `json:"context"`
	Selection     string                  `json:"backend"`
	Attempts      []initializationAttempt `json:"attempts"`
	MACKeyID      string                  `json:"macKeyId"`
	MAC           string                  `json:"mac"`
}

type initializationAttempt struct {
	KeyID      string `json:"keyId"`
	Generation string `json:"generation"`
}

type initializationAuthentication struct {
	Domain        string                  `json:"domain"`
	FormatVersion int                     `json:"formatVersion"`
	Context       string                  `json:"context"`
	Selection     string                  `json:"backend"`
	Attempts      []initializationAttempt `json:"attempts"`
	MACKeyID      string                  `json:"macKeyId"`
}

type identityRecord struct {
	FormatVersion int    `json:"formatVersion"`
	Context       string `json:"context"`
	ID            string `json:"id"`
}

type indexAdditionalData struct {
	Domain        string `json:"domain"`
	FormatVersion int    `json:"formatVersion"`
	Algorithm     string `json:"algorithm"`
	Context       string `json:"context"`
	Selection     string `json:"backend"`
	Generation    string `json:"generation"`
	KeyID         string `json:"keyId"`
	BlobID        string `json:"blobId"`
}

type partAdditionalData struct {
	Domain                 string       `json:"domain"`
	FormatVersion          int          `json:"formatVersion"`
	Algorithm              string       `json:"algorithm"`
	Context                string       `json:"context"`
	Selection              string       `json:"backend"`
	Generation             string       `json:"generation"`
	KeyID                  string       `json:"keyId"`
	BlobID                 string       `json:"blobId"`
	DeclarationFingerprint string       `json:"declarationFingerprint"`
	Name                   string       `json:"name"`
	Type                   string       `json:"type"`
	Source                 string       `json:"source"`
	Version                string       `json:"version"`
	Part                   secrets.Part `json:"part"`
}

func encodeCanonical(value any, maximum int) ([]byte, error) {
	size, err := canonicalEncodedSize(value, maximum)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(value)
	if err != nil || len(data)+1 != size {
		return nil, secretstore.Failure("store.limit", "secret store record exceeds its encoding limit")
	}
	return append(data, '\n'), nil
}

func canonicalEncodedSize(value any, maximum int) (int, error) {
	if maximum <= 0 {
		return 0, secretstore.Failure("store.limit", "secret store record exceeds its encoding limit")
	}
	size, valid := canonicalValueSize(reflect.ValueOf(value), maximum-1, 0)
	if !valid || size >= maximum {
		return 0, secretstore.Failure("store.limit", "secret store record exceeds its encoding limit")
	}
	return size + 1, nil
}

func canonicalValueSize(value reflect.Value, maximum, depth int) (int, bool) {
	if depth > 16 || maximum < 0 {
		return 0, false
	}
	if !value.IsValid() {
		return boundedSize(0, 4, maximum)
	}
	for value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return boundedSize(0, 4, maximum)
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Bool:
		if value.Bool() {
			return boundedSize(0, 4, maximum)
		}
		return boundedSize(0, 5, maximum)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var scratch [32]byte
		return boundedSize(0, len(strconv.AppendInt(scratch[:0], value.Int(), 10)), maximum)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		var scratch [32]byte
		return boundedSize(0, len(strconv.AppendUint(scratch[:0], value.Uint(), 10)), maximum)
	case reflect.String:
		return canonicalStringSize(value.String(), maximum)
	case reflect.Slice:
		if value.IsNil() {
			return boundedSize(0, 4, maximum)
		}
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return 0, false
		}
		return canonicalSequenceSize(value, maximum, depth)
	case reflect.Array:
		return canonicalSequenceSize(value, maximum, depth)
	case reflect.Struct:
		return canonicalStructSize(value, maximum, depth)
	default:
		return 0, false
	}
}

func canonicalSequenceSize(value reflect.Value, maximum, depth int) (int, bool) {
	size, valid := boundedSize(0, 2, maximum)
	for index := 0; valid && index < value.Len(); index++ {
		if index != 0 {
			size, valid = boundedSize(size, 1, maximum)
			if !valid {
				return 0, false
			}
		}
		var item int
		item, valid = canonicalValueSize(value.Index(index), maximum-size, depth+1)
		if valid {
			size, valid = boundedSize(size, item, maximum)
		}
	}
	return size, valid
}

func canonicalStructSize(value reflect.Value, maximum, depth int) (int, bool) {
	typeOf := value.Type()
	size, valid := boundedSize(0, 2, maximum)
	fields := 0
	for index := 0; valid && index < value.NumField(); index++ {
		field := typeOf.Field(index)
		if field.PkgPath != "" || field.Anonymous {
			return 0, false
		}
		tag := field.Tag.Get("json")
		name, options, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		// Only omissions this size calculation can predict exactly are
		// supported, because the predicted size must equal the encoding.
		// An omitted zero also lets a record written before a counted
		// field existed still round-trip byte for byte.
		if options == "omitempty" {
			fieldValue := value.Field(index)
			switch fieldValue.Kind() {
			case reflect.Bool:
				if !fieldValue.Bool() {
					continue
				}
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				if fieldValue.Int() == 0 {
					continue
				}
			default:
				return 0, false
			}
		} else if options != "" {
			return 0, false
		}
		if name == "" {
			name = field.Name
		}
		if fields != 0 {
			size, valid = boundedSize(size, 1, maximum)
			if !valid {
				return 0, false
			}
		}
		var nameSize int
		nameSize, valid = canonicalStringSize(name, maximum-size)
		if valid {
			size, valid = boundedSize(size, nameSize, maximum)
		}
		if valid {
			size, valid = boundedSize(size, 1, maximum)
		}
		var fieldSize int
		if valid {
			fieldSize, valid = canonicalValueSize(value.Field(index), maximum-size, depth+1)
		}
		if valid {
			size, valid = boundedSize(size, fieldSize, maximum)
		}
		fields++
	}
	return size, valid
}

func canonicalStringSize(value string, maximum int) (int, bool) {
	size, valid := boundedSize(0, 2, maximum)
	for index := 0; valid && index < len(value); {
		c := value[index]
		if c < utf8.RuneSelf {
			width := 1
			switch c {
			case '\\', '"', '\b', '\f', '\n', '\r', '\t':
				width = 2
			default:
				if c < 0x20 || c == '<' || c == '>' || c == '&' {
					width = 6
				}
			}
			size, valid = boundedSize(size, width, maximum)
			index++
			continue
		}
		r, consumed := utf8.DecodeRuneInString(value[index:])
		width := consumed
		if (r == utf8.RuneError && consumed == 1) || r == '\u2028' || r == '\u2029' {
			width = 6
		}
		size, valid = boundedSize(size, width, maximum)
		index += consumed
	}
	return size, valid
}

func boundedSize(current, additional, maximum int) (int, bool) {
	if current < 0 || additional < 0 || additional > maximum || current > maximum-additional {
		return 0, false
	}
	return current + additional, true
}

func decodeCanonical(data []byte, maximum, items int, target any) error {
	if len(data) == 0 || len(data) > maximum || !utf8.Valid(data) || !boundedJSON(data, items, maximum) || isIndexTarget(target) && !boundedIndexJSON(data) {
		return errors.New("invalid private record")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("invalid private record")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return errors.New("invalid private record")
	}
	canonical, err := encodeCanonical(target, maximum)
	if err != nil || !bytes.Equal(canonical, data) {
		return errors.New("noncanonical private record")
	}
	return nil
}

func isIndexTarget(target any) bool {
	_, ok := target.(*indexRecord)
	return ok
}

type indexJSONRole uint8

const (
	indexJSONGeneric indexJSONRole = iota
	indexJSONObject
	indexJSONKeys
	indexJSONVersions
	indexJSONCurrent
	indexJSONBindings
	indexJSONVersion
	indexJSONBinding
	indexJSONParts
	indexJSONBindingVersions
	indexJSONProduced
)

func boundedIndexJSON(data []byte) bool {
	type frame struct {
		kind       json.Delim
		role       indexJSONRole
		key        string
		items      int
		seen       uint8
		expectsKey bool
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	stack := make([]frame, 0, 8)
	root := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return root && len(stack) == 0
		}
		if err != nil {
			return false
		}
		if len(stack) == 0 {
			delimiter, ok := token.(json.Delim)
			if root || !ok || delimiter != '{' {
				return false
			}
			root = true
			stack = append(stack, frame{kind: delimiter, role: indexJSONObject, expectsKey: true})
			continue
		}
		current := &stack[len(stack)-1]
		if current.kind == '{' {
			if delimiter, ok := token.(json.Delim); ok && delimiter == '}' {
				if !current.expectsKey {
					return false
				}
				stack = stack[:len(stack)-1]
				continue
			}
			if current.expectsKey {
				key, ok := token.(string)
				if !ok {
					return false
				}
				current.key, current.expectsKey = key, false
				continue
			}
			role, field, valid := indexObjectChild(current.role, current.key)
			if !valid {
				return false
			}
			if field != 0 {
				if current.seen&field != 0 {
					return false
				}
				current.seen |= field
			}
			current.expectsKey = true
			if delimiter, ok := token.(json.Delim); ok {
				if delimiter != '{' && delimiter != '[' {
					return false
				}
				if role != indexJSONGeneric && delimiter != '[' {
					return false
				}
				stack = append(stack, frame{kind: delimiter, role: role, expectsKey: delimiter == '{'})
			} else if role != indexJSONGeneric {
				return false
			}
			continue
		}
		if delimiter, ok := token.(json.Delim); ok && delimiter == ']' {
			stack = stack[:len(stack)-1]
			continue
		}
		current.items++
		if limit := indexArrayLimit(current.role); limit >= 0 && current.items > limit {
			return false
		}
		if delimiter, ok := token.(json.Delim); ok {
			if delimiter != '{' && delimiter != '[' {
				return false
			}
			stack = append(stack, frame{kind: delimiter, role: indexArrayChild(current.role), expectsKey: delimiter == '{'})
		}
	}
}

func indexObjectChild(parent indexJSONRole, key string) (indexJSONRole, uint8, bool) {
	if parent == indexJSONObject {
		switch key {
		case "keys":
			return indexJSONKeys, 1 << 0, true
		case "versions":
			return indexJSONVersions, 1 << 1, true
		case "current":
			return indexJSONCurrent, 1 << 2, true
		case "bindings":
			return indexJSONBindings, 1 << 3, true
		case "produced":
			return indexJSONProduced, 1 << 4, true
		}
		if strings.EqualFold(key, "keys") || strings.EqualFold(key, "versions") || strings.EqualFold(key, "current") || strings.EqualFold(key, "bindings") || strings.EqualFold(key, "produced") {
			return indexJSONGeneric, 0, false
		}
	}
	if parent == indexJSONVersion {
		if key == "parts" {
			return indexJSONParts, 1 << 0, true
		}
		if strings.EqualFold(key, "parts") {
			return indexJSONGeneric, 0, false
		}
	}
	if parent == indexJSONBinding {
		if key == "versions" {
			return indexJSONBindingVersions, 1 << 0, true
		}
		if strings.EqualFold(key, "versions") {
			return indexJSONGeneric, 0, false
		}
	}
	return indexJSONGeneric, 0, true
}

func indexArrayChild(parent indexJSONRole) indexJSONRole {
	switch parent {
	case indexJSONVersions:
		return indexJSONVersion
	case indexJSONBindings:
		return indexJSONBinding
	}
	return indexJSONGeneric
}

func indexArrayLimit(role indexJSONRole) int {
	switch role {
	case indexJSONKeys:
		return maxPhysicalItems
	case indexJSONVersions, indexJSONCurrent, indexJSONBindings, indexJSONBindingVersions, indexJSONProduced:
		return secrets.MaxVersions
	case indexJSONParts:
		return 2
	}
	return -1
}

// boundedJSON limits structure before encoding/json allocates typed slices or
// attacker-selected strings. Canonical comparison remains the grammar check.
func boundedJSON(data []byte, maximumItems, maximumString int) bool {
	type frame struct {
		kind   byte
		commas int
	}
	stack := make([]frame, 0, 8)
	quoted, escaped, start := false, false, 0
	items := 0
	for index, c := range data {
		if quoted {
			if index-start > maximumString {
				return false
			}
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		switch c {
		case '"':
			quoted, start = true, index
		case '{', '[':
			if len(stack) >= 12 {
				return false
			}
			stack = append(stack, frame{kind: c})
		case '}', ']':
			if len(stack) == 0 {
				return false
			}
			stack = stack[:len(stack)-1]
		case ',':
			items++
			if items > maximumItems || len(stack) == 0 {
				return false
			}
			stack[len(stack)-1].commas++
		case 'n':
			if bytes.HasPrefix(data[index:], []byte("null")) {
				// Optional declaration slices use null in Go's zero value, so
				// canonical null is accepted and constrained by typed validation.
				continue
			}
		}
	}
	return !quoted && !escaped && len(stack) == 0
}

func seal(key, plaintext, additional []byte, purpose, keyID, blobID string, random io.Reader, maximum int) ([]byte, error) {
	if len(key) != 32 {
		return nil, secretstore.Failure("store.crypto", "secret encryption key is invalid")
	}
	if _, err := sealedEnvelopeSize(len(plaintext), purpose, keyID, blobID, maximum); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, secretstore.Failure("store.crypto", "secret encryption key is invalid")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil || aead.NonceSize() != gcmNonceSize || aead.Overhead() != gcmTagSize {
		return nil, secretstore.Failure("store.crypto", "AES-256-GCM is unavailable")
	}
	nonce := make([]byte, aead.NonceSize())
	if random == nil {
		return nil, secretstore.Failure("store.crypto", "secret encryption randomness is unavailable")
	}
	if _, err := io.ReadFull(random, nonce); err != nil {
		clear(nonce)
		return nil, secretstore.Failure("store.crypto", "secret encryption randomness is unavailable")
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, additional)
	record := envelope{FormatVersion: formatVersion, Algorithm: algorithm, Purpose: purpose, KeyID: keyID, BlobID: blobID, Nonce: rawBase64.EncodeToString(nonce), Ciphertext: rawBase64.EncodeToString(ciphertext)}
	clear(nonce)
	clear(ciphertext)
	return encodeCanonical(record, maximum)
}

func sealedEnvelopeSize(plaintext int, purpose, keyID, blobID string, maximum int) (int, error) {
	if plaintext < 0 || maximum <= gcmTagSize || plaintext > maximum-gcmTagSize {
		return 0, secretstore.Failure("store.limit", "encrypted secret artifact exceeds its encoding limit")
	}
	base, err := canonicalEncodedSize(envelope{FormatVersion: formatVersion, Algorithm: algorithm, Purpose: purpose, KeyID: keyID, BlobID: blobID, Nonce: "AAAAAAAAAAAAAAAA", Ciphertext: ""}, maximum)
	if err != nil {
		return 0, secretstore.Failure("store.limit", "encrypted secret artifact exceeds its encoding limit")
	}
	ciphertext := rawBase64.EncodedLen(plaintext + gcmTagSize)
	size, valid := boundedSize(base, ciphertext, maximum)
	if !valid {
		return 0, secretstore.Failure("store.limit", "encrypted secret artifact exceeds its encoding limit")
	}
	return size, nil
}

func openEnvelope(data, key, additional []byte, purpose, keyID, blobID string, maximum int) ([]byte, error) {
	if len(key) != 32 {
		return nil, secretstore.Failure("store.crypto", "secret encryption key is invalid")
	}
	var record envelope
	if err := decodeCanonical(data, maximum, 32, &record); err != nil || record.FormatVersion != formatVersion || record.Algorithm != algorithm || record.Purpose != purpose || record.KeyID != keyID || record.BlobID != blobID {
		return nil, secretstore.Failure("store.corrupt", "encrypted secret artifact is malformed or incompatible")
	}
	nonce, err := decodeBase64(record.Nonce, gcmNonceSize)
	if err != nil || len(nonce) != gcmNonceSize {
		clear(nonce)
		return nil, secretstore.Failure("store.corrupt", "encrypted secret artifact nonce is invalid")
	}
	ciphertext, err := decodeBase64(record.Ciphertext, maximum)
	if err != nil || len(ciphertext) < gcmTagSize {
		clear(nonce)
		clear(ciphertext)
		return nil, secretstore.Failure("store.corrupt", "encrypted secret artifact ciphertext is invalid")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		clear(nonce)
		clear(ciphertext)
		return nil, secretstore.Failure("store.crypto", "secret encryption key is invalid")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		clear(nonce)
		clear(ciphertext)
		return nil, secretstore.Failure("store.crypto", "AES-256-GCM is unavailable")
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, additional)
	clear(nonce)
	clear(ciphertext)
	if err != nil {
		clear(plaintext)
		return nil, secretstore.Failure("store.crypto", "encrypted secret artifact authentication failed")
	}
	return plaintext, nil
}

func decodeBase64(value string, maximum int) ([]byte, error) {
	if value == "" || len(value) > rawBase64.EncodedLen(maximum) {
		return nil, errors.New("invalid base64")
	}
	decoded := make([]byte, rawBase64.DecodedLen(len(value)))
	n, err := rawBase64.Decode(decoded, []byte(value))
	if err != nil || rawBase64.EncodeToString(decoded[:n]) != value {
		clear(decoded)
		return nil, errors.New("invalid base64")
	}
	return decoded[:n], nil
}

func indexAAD(contextName string, selector secretstore.Selector, keyID string) []byte {
	data, _ := json.Marshal(indexAdditionalData{Domain: "bootwright.secret.index.v4", FormatVersion: formatVersion, Algorithm: algorithm, Context: contextName, Selection: selector.Backend, Generation: selector.Generation, KeyID: keyID, BlobID: selector.Generation})
	return data
}

func partAAD(contextName string, selection string, version storedVersion, part storedPart) []byte {
	data, _ := json.Marshal(partAdditionalData{Domain: "bootwright.secret.part.v4", FormatVersion: formatVersion, Algorithm: algorithm, Context: contextName, Selection: selection, Generation: part.Generation, KeyID: part.KeyID, BlobID: part.BlobID, DeclarationFingerprint: version.Declaration.Fingerprint, Name: version.Declaration.Name, Type: version.Declaration.Type, Source: version.Declaration.Source, Version: version.ID, Part: part.Part})
	return data
}

func encodeLedger(contextName string, selection string, key []byte, keyID string, seals uint64) ([]byte, error) {
	record := sealLedger{FormatVersion: formatVersion, KeyID: keyID, Seals: seals}
	record.MAC = ledgerMAC(contextName, selection, key, keyID, seals)
	return encodeCanonical(record, ledgerMaximum)
}

func decodeLedger(data []byte, contextName string, selection string, key []byte, keyID string, floor uint64) (sealLedger, error) {
	var record sealLedger
	if decodeCanonical(data, ledgerMaximum, 24, &record) != nil || record.FormatVersion != formatVersion || record.KeyID != keyID || record.Seals < floor || record.Seals > maxSeals || record.MAC == "" {
		return sealLedger{}, errors.New("invalid seal ledger")
	}
	expected := ledgerMAC(contextName, selection, key, keyID, record.Seals)
	actual, err := decodeBase64(record.MAC, sha256.Size)
	want, wantErr := decodeBase64(expected, sha256.Size)
	valid := err == nil && wantErr == nil && hmac.Equal(actual, want)
	clear(actual)
	clear(want)
	if !valid {
		return sealLedger{}, errors.New("invalid seal ledger authentication")
	}
	return record, nil
}

func ledgerMAC(contextName string, selection string, key []byte, keyID string, seals uint64) string {
	derive := hmac.New(sha256.New, key)
	derive.Write([]byte("bootwright.secret.ledger.mac-key.v2"))
	macKey := derive.Sum(nil)
	data, _ := json.Marshal(ledgerAuthentication{Domain: "bootwright.secret.ledger.v4", FormatVersion: formatVersion, Algorithm: "HMAC-SHA256", Context: contextName, Selection: selection, KeyID: keyID, Seals: seals})
	mac := hmac.New(sha256.New, macKey)
	mac.Write(data)
	result := rawBase64.EncodeToString(mac.Sum(nil))
	clear(macKey)
	return result
}

func signInitialization(record initializationRecord, keyID string, key []byte) initializationRecord {
	record.MACKeyID = keyID
	record.MAC = initializationMAC(record, key)
	return record
}

func verifyInitializationMAC(record initializationRecord, key []byte) bool {
	if record.MAC == "" || record.MACKeyID == "" {
		return false
	}
	expected := initializationMAC(record, key)
	actualBytes, actualErr := decodeBase64(record.MAC, sha256.Size)
	expectedBytes, expectedErr := decodeBase64(expected, sha256.Size)
	valid := actualErr == nil && expectedErr == nil && hmac.Equal(actualBytes, expectedBytes)
	clear(actualBytes)
	clear(expectedBytes)
	return valid
}

func initializationMAC(record initializationRecord, key []byte) string {
	derive := hmac.New(sha256.New, key)
	derive.Write([]byte("bootwright.secret.initialization.mac-key.v2"))
	macKey := derive.Sum(nil)
	data, _ := json.Marshal(initializationAuthentication{Domain: "bootwright.secret.initialization.v4", FormatVersion: record.FormatVersion, Context: record.Context, Selection: record.Selection, Attempts: record.Attempts, MACKeyID: record.MACKeyID})
	mac := hmac.New(sha256.New, macKey)
	mac.Write(data)
	result := rawBase64.EncodeToString(mac.Sum(nil))
	clear(macKey)
	return result
}

func validID(value, prefix string) bool {
	if len(value) != len(prefix)+32 || !strings.HasPrefix(value, prefix) {
		return false
	}
	decoded, err := hex.DecodeString(value[len(prefix):])
	return err == nil && hex.EncodeToString(decoded) == value[len(prefix):]
}

func validName(value string) bool {
	if len(value) == 0 || len(value) > 63 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

func validFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && hex.EncodeToString(decoded) == value
}

func declarationFingerprint(declaration secrets.Declaration) string {
	declaration.Fingerprint = ""
	canonical, err := json.Marshal(declaration)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:])
}

func validateDeclaration(declaration secrets.Declaration) bool {
	if !validName(declaration.Name) || !validFingerprint(declaration.Fingerprint) || declarationFingerprint(declaration) != declaration.Fingerprint || len(declaration.Origin) > 4096 || declaration.Document < 0 {
		return false
	}
	switch declaration.Source {
	case "contextStore":
		if !reflect.DeepEqual(declaration.Files, secrets.FileSource{}) || !reflect.DeepEqual(declaration.Generation, secrets.Generation{}) {
			return false
		}
	case "generated":
		if !reflect.DeepEqual(declaration.Files, secrets.FileSource{}) {
			return false
		}
	default:
		return false
	}
	parts := declaration.Parts()
	return len(parts) > 0 && len(parts) <= 2
}

func validateMaterial(declaration secrets.Declaration, material secrets.Material) error {
	parts := material.Parts()
	expected := declaration.Parts()
	if !slices.Equal(parts, expected) || material.Size() > secrets.MaxVersionBytes {
		return secretstore.Failure("part", "secret material does not contain the exact declared parts")
	}
	for _, part := range parts {
		value, exists := material.Part(part)
		if !exists || len(value) > secrets.MaxPartBytes {
			clear(value)
			return secretstore.Failure("part", "secret material part exceeds its bound")
		}
		clear(value)
	}
	return nil
}

func interrupted(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func validateVersionDeclaration(declaration secrets.VersionDeclaration) bool {
	return validName(declaration.Name) && validFingerprint(declaration.Fingerprint) && (declaration.Source == "contextStore" || declaration.Source == "file" || declaration.Source == "generated" || declaration.Source == producedSource) && len(declaration.Parts()) > 0 && len(declaration.Parts()) <= 2
}

// producedSource marks a version a lifecycle block captured. No Secret
// declaration can name it, so no secret command reaches one.
const (
	producedSource = "produced"
	producedType   = "opaque"
	producedDomain = "bootwright.secret.produced.v4"
)

type producedIdentity struct {
	Domain string `json:"domain"`
	Block  string `json:"block"`
	Name   string `json:"name"`
}

// producedDeclaration is the summary a produced version carries. Its
// fingerprint covers the block and name that key it, so the version cannot be
// moved to another entry.
func producedDeclaration(block, name string) secrets.VersionDeclaration {
	data, _ := json.Marshal(producedIdentity{Domain: producedDomain, Block: block, Name: name})
	digest := sha256.Sum256(data)
	return secrets.VersionDeclaration{Name: name, Type: producedType, Source: producedSource, Fingerprint: hex.EncodeToString(digest[:])}
}
