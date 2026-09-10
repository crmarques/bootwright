package localstore

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/storage"
)

const (
	legacySelectorPath = "selector.json"
	upgradePath        = "upgrade.json"
	maxUpgradeAttempts = 16
)

type legacySelector struct {
	SelectorVersion int               `json:"selectorVersion"`
	ContextID       string            `json:"contextId"`
	Selection       storage.Selection `json:"implementation"`
	Generation      string            `json:"generation"`
}

type legacyIndex struct {
	FormatVersion int               `json:"formatVersion"`
	Algorithm     string            `json:"algorithm"`
	Selector      legacySelector    `json:"selector"`
	ActiveKey     string            `json:"activeKey"`
	Keys          []storage.Key     `json:"keys"`
	Versions      []legacyVersion   `json:"versions"`
	Current       []storage.Current `json:"current"`
	Bindings      []storage.Binding `json:"bindings"`
}

type legacyVersion struct {
	ID          string              `json:"id"`
	Declaration secrets.Declaration `json:"declaration"`
	Parts       []storedPart        `json:"parts"`
}

type legacyIndexAAD struct {
	Domain        string            `json:"domain"`
	FormatVersion int               `json:"formatVersion"`
	Algorithm     string            `json:"algorithm"`
	ContextID     string            `json:"contextId"`
	Selection     storage.Selection `json:"implementation"`
	Generation    string            `json:"generation"`
	KeyID         string            `json:"keyId"`
	BlobID        string            `json:"blobId"`
}

type legacyPartAAD struct {
	Domain                 string            `json:"domain"`
	FormatVersion          int               `json:"formatVersion"`
	Algorithm              string            `json:"algorithm"`
	ContextID              string            `json:"contextId"`
	Selection              storage.Selection `json:"implementation"`
	Generation             string            `json:"generation"`
	KeyID                  string            `json:"keyId"`
	BlobID                 string            `json:"blobId"`
	DeclarationFingerprint string            `json:"declarationFingerprint"`
	Name                   string            `json:"name"`
	Type                   string            `json:"type"`
	Source                 string            `json:"source"`
	Version                string            `json:"version"`
	Part                   secrets.Part      `json:"part"`
}

type legacyLedgerAuthentication struct {
	Domain        string            `json:"domain"`
	FormatVersion int               `json:"formatVersion"`
	Algorithm     string            `json:"algorithm"`
	ContextID     string            `json:"contextId"`
	Selection     storage.Selection `json:"implementation"`
	KeyID         string            `json:"keyId"`
	Seals         uint64            `json:"seals"`
}

type legacyInitialization struct {
	FormatVersion int                           `json:"formatVersion"`
	ContextID     string                        `json:"contextId"`
	Selection     storage.Selection             `json:"implementation"`
	Attempts      []legacyInitializationAttempt `json:"attempts"`
	MACKeyID      string                        `json:"macKeyId"`
	MAC           string                        `json:"mac"`
}

type legacyInitializationAttempt struct {
	AttemptID  string `json:"attemptId"`
	KeyID      string `json:"keyId"`
	Generation string `json:"generation"`
}

type legacyInitializationAuthentication struct {
	Domain        string                        `json:"domain"`
	FormatVersion int                           `json:"formatVersion"`
	ContextID     string                        `json:"contextId"`
	Selection     storage.Selection             `json:"implementation"`
	Attempts      []legacyInitializationAttempt `json:"attempts"`
	MACKeyID      string                        `json:"macKeyId"`
}

type upgradeAttempt struct {
	KeyID      string `json:"keyId"`
	Generation string `json:"generation"`
}

type upgradeRecord struct {
	Version        int              `json:"version"`
	ContextID      string           `json:"contextId"`
	Backend        string           `json:"backend"`
	SourceSelector string           `json:"sourceSelector"`
	Attempts       []upgradeAttempt `json:"attempts"`
	MAC            string           `json:"mac"`
}

type legacyState struct {
	selector legacySelector
	data     []byte
	index    legacyIndex
	keys     map[string][]byte
	plain    []plainPart
}

func legacySelection() storage.Selection {
	return storage.Selection{
		Type:       "local-keyring",
		Store:      storage.ComponentRef{ID: "local-v1", InterfaceVersion: 1, StateVersion: 1, ConfigVersion: 1},
		KeyCustody: storage.ComponentRef{ID: "local-keyfile-v1", InterfaceVersion: 1, StateVersion: 1, ConfigVersion: 1},
	}
}

// upgrade authenticates the complete legacy source before recording intent or
// acquiring new custody. Old artifacts remain unchanged until store publication.
func (i *Implementation) upgrade(ctx context.Context, selected storage.Context, area storage.Area) (*session, error) {
	before, present, err := area.ReadMutable(ctx, selectorPath, storage.RecordMaximum)
	if err != nil || present {
		return nil, areaFailure(ctx, "store.conflict", "secret metadata already exists before upgrade", err)
	}
	old, err := readLegacyState(ctx, selected, area)
	if err != nil {
		return nil, err
	}
	defer old.clear()
	journalData, exists, err := area.ReadMutable(ctx, upgradePath, selectorMaximum)
	if err != nil {
		return nil, areaFailure(ctx, "store.corrupt", "secret upgrade intent is unsafe", err)
	}
	digest := sha256.Sum256(old.data)
	journal := upgradeRecord{Version: formatVersion, ContextID: selected.ID, Backend: i.Backend(), SourceSelector: hex.EncodeToString(digest[:]), Attempts: []upgradeAttempt{}}
	if exists {
		if decodeCanonical(journalData, selectorMaximum, 256, &journal) != nil || !validUpgrade(journal, selected.ID, i.Backend(), digest, old.keys[old.index.ActiveKey]) {
			return nil, storage.Failure("store.corrupt", "secret upgrade intent is invalid or does not match its source")
		}
	}
	if err := verifyLegacyLayout(ctx, area, old, journal); err != nil {
		return nil, err
	}
	if exists {
		paths, err := unpublishedUpgradeArtifacts(ctx, area, old, journal)
		if err != nil {
			return nil, err
		}
		guards := []storage.RecordExpectation{{Path: legacySelectorPath, Data: old.data}, {Path: upgradePath, Data: journalData}}
		publications, artifacts := []string{}, []string{}
		for _, path := range paths {
			if strings.Contains(path, "/") {
				artifacts = append(artifacts, path)
			} else {
				publications = append(publications, path)
			}
		}
		// A surviving encrypted temporary still needs its attempt key for
		// attribution. Remove such publications durably before retiring keys.
		for _, pending := range [][]string{publications, artifacts} {
			if len(pending) != 0 {
				if err := area.PruneUnpublished(ctx, guards, pending); err != nil {
					return nil, areaFailure(ctx, "store.conflict", "interrupted secret upgrade cleanup is incomplete; retry secret encryption init", err)
				}
			}
		}
	}
	keyNames, err := entryNames(ctx, area, "keys")
	if err != nil {
		return nil, err
	}
	keyID, err := i.uniqueID("key-", func(id string) bool {
		if keyNames[id+".key"] || keyNames[id+".usage.json"] || keyNames[id+".bin"] {
			return true
		}
		for _, attempt := range journal.Attempts {
			if attempt.KeyID == id {
				return true
			}
		}
		return false
	})
	if err != nil {
		return nil, err
	}
	generation, err := i.uniqueID("gen-", func(id string) bool {
		if id == old.selector.Generation {
			return true
		}
		for _, attempt := range journal.Attempts {
			if attempt.Generation == id {
				return true
			}
		}
		return false
	})
	if err != nil {
		return nil, err
	}
	attempt := upgradeAttempt{KeyID: keyID, Generation: generation}
	next := old.summary(storage.Selector{SelectorVersion: formatVersion, ContextID: selected.ID, Backend: i.Backend(), Generation: generation})
	next.Legacy = true
	next.ActiveKey = keyID
	next.Keys = []storedKey{{ID: keyID, Seals: uint64(len(old.plain)) + 1}}
	partNames, err := entryNames(ctx, area, "parts")
	if err != nil {
		return nil, err
	}
	for versionIndex := range next.Versions {
		for partIndex := range next.Versions[versionIndex].Parts {
			part := &next.Versions[versionIndex].Parts[partIndex]
			part.BlobID = upgradeBlobID(attempt, part.BlobID)
			if partNames[part.BlobID+".enc"] {
				return nil, storage.Failure("store.conflict", "secret upgrade artifact identity is already reserved")
			}
			partNames[part.BlobID+".enc"] = true
			part.KeyID, part.Generation = keyID, generation
		}
	}
	if err := validateIndex(next, next.Selector); err != nil {
		return nil, storage.Failure("store.corrupt", "legacy secret metadata cannot be represented safely")
	}
	plaintext, err := encodeCanonical(next, indexMaximum)
	if err != nil {
		return nil, err
	}
	defer clear(plaintext)
	if _, err := metadataEncodedSize(len(plaintext), next.Selector, keyID); err != nil {
		return nil, err
	}
	for _, version := range next.Versions {
		for _, part := range version.Parts {
			if _, err := sealedEnvelopeSize(part.Size, "part", keyID, part.BlobID, partMaximum); err != nil {
				return nil, err
			}
		}
	}
	// Prior attempts cease to authorize artifacts only after their complete,
	// verified removal is durable. No new logical secret identities were issued.
	journal.Attempts = []upgradeAttempt{attempt}
	journal.MAC = upgradeMAC(journal, old.keys[old.index.ActiveKey])
	intent, err := encodeCanonical(journal, selectorMaximum)
	if err != nil {
		return nil, err
	}
	if err := preflightUpgradeCapacity(ctx, area, next, len(plaintext), intent, journalData); err != nil {
		return nil, err
	}
	outcome, err := area.Replace(ctx, upgradePath, intent, journalData)
	if err != nil || outcome != storage.Committed {
		return nil, publicationFailure(ctx, outcome, err)
	}
	currentIntent, present, err := area.ReadMutable(ctx, upgradePath, selectorMaximum)
	if err != nil || !present || !bytes.Equal(currentIntent, intent) {
		return nil, areaFailure(ctx, "store.conflict", "secret upgrade intent changed after publication", err)
	}
	key := make([]byte, 32)
	keepKey := false
	defer func() {
		if !keepKey {
			clear(key)
		}
	}()
	if _, err := io.ReadFull(i.random, key); err != nil {
		return nil, storage.Failure("store.crypto", "secret upgrade randomness is unavailable")
	}
	if err := area.WriteExclusive(ctx, keyPath(keyID), key); err != nil {
		return nil, areaFailure(ctx, "store.conflict", "secret upgrade key could not be stored", err)
	}
	usage, err := encodeLedger(selected.ID, i.Backend(), key, keyID, next.Keys[0].Seals)
	if err != nil {
		return nil, err
	}
	if err := area.WriteExclusive(ctx, ledgerPath(keyID), usage); err != nil {
		return nil, areaFailure(ctx, "store.conflict", "secret upgrade seal reservation could not be stored", err)
	}
	for _, pending := range old.plain {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		version, _ := findVersion(next, pending.version)
		part, _ := storedPartOf(version, pending.part)
		sealed, err := seal(key, pending.data, partAAD(selected.ID, i.Backend(), version, part), "part", keyID, part.BlobID, i.random, partMaximum)
		if err != nil {
			return nil, err
		}
		err = area.WriteExclusive(ctx, partPath(part.BlobID), sealed)
		clear(sealed)
		if err != nil {
			return nil, areaFailure(ctx, "store.conflict", "secret upgrade part could not be stored", err)
		}
	}
	metadata, err := sealMetadata(key, plaintext, next.Selector, keyID, i.random)
	if err != nil {
		return nil, err
	}
	defer clear(metadata)
	current, present, err := area.Read(ctx, legacySelectorPath, selectorMaximum)
	if err != nil || !present || !bytes.Equal(current, old.data) {
		return nil, areaFailure(ctx, "store.conflict", "legacy secret selection changed during upgrade", err)
	}
	currentIntent, present, err = area.Read(ctx, upgradePath, selectorMaximum)
	if err != nil || !present || !bytes.Equal(currentIntent, intent) {
		return nil, areaFailure(ctx, "store.conflict", "secret upgrade intent changed before publication", err)
	}
	_, present, err = area.Read(ctx, selectorPath, storage.RecordMaximum)
	if err != nil || present {
		return nil, areaFailure(ctx, "store.conflict", "secret metadata appeared during upgrade", err)
	}
	outcome, err = area.Replace(ctx, selectorPath, metadata, before)
	if err != nil || outcome != storage.Committed {
		return nil, publicationFailure(ctx, outcome, err)
	}
	s := &session{implementation: i, context: selected, area: area, selector: next.Selector, selectorData: slices.Clone(metadata), index: next, keys: map[string][]byte{keyID: key}}
	keepKey = true
	if err := s.collectArtifacts(ctx); err != nil {
		s.Close()
		return nil, cleanupFailure(ctx, err)
	}
	return s, nil
}

func (s *legacyState) clear() {
	for _, key := range s.keys {
		clear(key)
	}
	clearPlain(s.plain)
}

func (s *legacyState) summary(selector storage.Selector) indexRecord {
	index := indexRecord{FormatVersion: formatVersion, Algorithm: algorithm, Selector: selector, ActiveKey: s.index.ActiveKey, Keys: make([]storedKey, len(s.index.Keys)), Versions: make([]storedVersion, len(s.index.Versions)), Current: slices.Clone(s.index.Current), Bindings: cloneBindings(s.index.Bindings)}
	for n, key := range s.index.Keys {
		index.Keys[n] = storedKey{ID: key.ID, Seals: key.Seals}
	}
	for n, version := range s.index.Versions {
		index.Versions[n] = storedVersion{ID: version.ID, Declaration: version.Declaration.Summary(), Parts: slices.Clone(version.Parts)}
	}
	return index
}

func readLegacyState(ctx context.Context, selected storage.Context, area storage.Area) (*legacyState, error) {
	s := &legacyState{keys: map[string][]byte{}}
	succeeded := false
	defer func() {
		if !succeeded {
			s.clear()
		}
	}()
	var exists bool
	var err error
	s.data, exists, err = area.ReadMutable(ctx, legacySelectorPath, selectorMaximum)
	if err != nil || !exists || decodeCanonical(s.data, selectorMaximum, 64, &s.selector) != nil || s.selector.SelectorVersion != 1 || s.selector.ContextID != selected.ID || s.selector.Selection != legacySelection() || !validID(s.selector.Generation, "gen-") {
		return nil, areaFailure(ctx, "store.corrupt", "legacy secret selection is invalid or incompatible", err)
	}
	sealed, exists, err := area.ReadMutable(ctx, "indexes/"+s.selector.Generation+".bin", indexMaximum)
	if err != nil || !exists {
		return nil, areaFailure(ctx, "store.corrupt", "legacy secret index is missing or unsafe", err)
	}
	defer clear(sealed)
	var wrapped envelope
	if decodeCanonical(sealed, indexMaximum, 32, &wrapped) != nil || wrapped.FormatVersion != 1 || wrapped.Algorithm != algorithm || wrapped.Purpose != "index" || wrapped.BlobID != s.selector.Generation || !validID(wrapped.KeyID, "key-") {
		return nil, storage.Failure("store.corrupt", "legacy secret index envelope is invalid")
	}
	key, err := s.key(ctx, area, wrapped.KeyID)
	if err != nil {
		return nil, err
	}
	additional, _ := json.Marshal(legacyIndexAAD{Domain: "bootwright.secret.index.v1", FormatVersion: 1, Algorithm: algorithm, ContextID: selected.ID, Selection: s.selector.Selection, Generation: s.selector.Generation, KeyID: wrapped.KeyID, BlobID: s.selector.Generation})
	plaintext, err := openLegacyEnvelope(sealed, key, additional, "index", wrapped.KeyID, s.selector.Generation, indexMaximum)
	if err != nil {
		return nil, err
	}
	defer clear(plaintext)
	if !boundedIndexJSON(plaintext) || decodeCanonical(plaintext, indexMaximum, maxIndexItems, &s.index) != nil || s.index.FormatVersion != 1 || s.index.Algorithm != algorithm || s.index.Selector != s.selector || s.index.ActiveKey != wrapped.KeyID || s.index.Keys == nil || s.index.Versions == nil || s.index.Current == nil || s.index.Bindings == nil {
		return nil, storage.Failure("store.corrupt", "legacy secret index is invalid or inconsistent")
	}
	for _, version := range s.index.Versions {
		if !validateDeclaration(version.Declaration) {
			return nil, storage.Failure("store.corrupt", "legacy secret declaration identity is invalid")
		}
	}
	selector := storage.Selector{SelectorVersion: formatVersion, ContextID: selected.ID, Backend: "local-keyring-v2", Generation: s.selector.Generation}
	if err := validateIndex(s.summary(selector), selector); err != nil {
		return nil, storage.Failure("store.corrupt", "legacy secret index references are invalid")
	}
	for _, reference := range s.index.Keys {
		if reference.State != "active" && reference.State != "retired" || (reference.State == "active") != (reference.ID == s.index.ActiveKey) {
			return nil, storage.Failure("store.corrupt", "legacy secret key state is inconsistent")
		}
		key, err := s.key(ctx, area, reference.ID)
		if err != nil {
			return nil, err
		}
		data, exists, err := area.ReadMutable(ctx, "ledgers/"+reference.ID+".json", ledgerMaximum)
		if err != nil || !exists {
			return nil, areaFailure(ctx, "store.corrupt", "legacy secret seal ledger is missing or unsafe", err)
		}
		var ledger sealLedger
		if decodeCanonical(data, ledgerMaximum, 24, &ledger) != nil || ledger.FormatVersion != 1 || ledger.KeyID != reference.ID || ledger.Seals < reference.Seals || ledger.Seals > maxSeals {
			return nil, storage.Failure("store.corrupt", "legacy secret seal ledger is invalid or contradictory")
		}
		authentication, _ := json.Marshal(legacyLedgerAuthentication{Domain: "bootwright.secret.ledger.v1", FormatVersion: 1, Algorithm: "HMAC-SHA256", ContextID: selected.ID, Selection: s.selector.Selection, KeyID: reference.ID, Seals: ledger.Seals})
		if !verifyLegacyMAC(key, "bootwright.secret.ledger.mac-key.v1", authentication, ledger.MAC) {
			return nil, storage.Failure("store.corrupt", "legacy secret seal ledger authentication failed")
		}
	}
	if err := s.verifyInitialization(ctx, area); err != nil {
		return nil, err
	}
	for _, version := range s.index.Versions {
		for _, part := range version.Parts {
			key, err := s.key(ctx, area, part.KeyID)
			if err != nil {
				return nil, err
			}
			data, exists, err := area.ReadMutable(ctx, "parts/"+part.BlobID+".bin", partMaximum)
			if err != nil || !exists {
				return nil, areaFailure(ctx, "store.corrupt", "legacy secret part is missing or unsafe", err)
			}
			aad, _ := json.Marshal(legacyPartAAD{Domain: "bootwright.secret.part.v1", FormatVersion: 1, Algorithm: algorithm, ContextID: selected.ID, Selection: s.selector.Selection, Generation: part.Generation, KeyID: part.KeyID, BlobID: part.BlobID, DeclarationFingerprint: version.Declaration.Fingerprint, Name: version.Declaration.Name, Type: version.Declaration.Type, Source: version.Declaration.Source, Version: version.ID, Part: part.Part})
			material, err := openLegacyEnvelope(data, key, aad, "part", part.KeyID, part.BlobID, partMaximum)
			clear(data)
			if err != nil {
				return nil, err
			}
			if len(material) != part.Size {
				clear(material)
				return nil, storage.Failure("store.corrupt", "legacy secret part size is inconsistent")
			}
			s.plain = append(s.plain, plainPart{version: version.ID, part: part.Part, data: material})
		}
	}
	succeeded = true
	return s, nil
}

func (s *legacyState) key(ctx context.Context, area storage.Area, id string) ([]byte, error) {
	if key, exists := s.keys[id]; exists {
		return key, nil
	}
	if !validID(id, "key-") {
		return nil, storage.Failure("store.corrupt", "legacy secret key identity is invalid")
	}
	key, exists, err := area.ReadMutable(ctx, "keys/"+id+".bin", 32)
	if err != nil || !exists || len(key) != 32 {
		clear(key)
		return nil, areaFailure(ctx, "store.key-unavailable", "legacy secret encryption key is missing or unsafe", err)
	}
	s.keys[id] = key
	return key, nil
}

func (s *legacyState) verifyInitialization(ctx context.Context, area storage.Area) error {
	data, exists, err := area.ReadMutable(ctx, initializationPath, selectorMaximum)
	if err != nil || !exists {
		return areaFailure(ctx, "store.corrupt", "legacy secret initialization record is missing or unsafe", err)
	}
	var marker legacyInitialization
	if decodeCanonical(data, selectorMaximum, 512, &marker) != nil || marker.FormatVersion != 1 || marker.ContextID != s.selector.ContextID || marker.Selection != s.selector.Selection || marker.MAC == "" || !validID(marker.MACKeyID, "key-") || len(marker.Attempts) == 0 || len(marker.Attempts) > 16 {
		return storage.Failure("store.corrupt", "legacy secret initialization record is invalid")
	}
	seen := map[string]bool{}
	macKeySeen := false
	for _, attempt := range marker.Attempts {
		if !validID(attempt.AttemptID, "init-") || !validID(attempt.KeyID, "key-") || !validID(attempt.Generation, "gen-") || seen[attempt.AttemptID] || seen[attempt.KeyID] || seen[attempt.Generation] {
			return storage.Failure("store.corrupt", "legacy secret initialization attempts are invalid")
		}
		seen[attempt.AttemptID], seen[attempt.KeyID], seen[attempt.Generation] = true, true, true
		macKeySeen = macKeySeen || attempt.KeyID == marker.MACKeyID
	}
	if !macKeySeen {
		return storage.Failure("store.corrupt", "legacy secret initialization signing key is invalid")
	}
	key, err := s.key(ctx, area, marker.MACKeyID)
	if err != nil {
		return err
	}
	authentication, _ := json.Marshal(legacyInitializationAuthentication{Domain: "bootwright.secret.initialization.v1", FormatVersion: 1, ContextID: marker.ContextID, Selection: marker.Selection, Attempts: marker.Attempts, MACKeyID: marker.MACKeyID})
	if !verifyLegacyMAC(key, "bootwright.secret.initialization.mac-key.v1", authentication, marker.MAC) {
		return storage.Failure("store.corrupt", "legacy secret initialization authentication failed")
	}
	return nil
}

func openLegacyEnvelope(data, key, aad []byte, purpose, keyID, blobID string, maximum int) ([]byte, error) {
	var record envelope
	if len(key) != 32 || decodeCanonical(data, maximum, 32, &record) != nil || record.FormatVersion != 1 || record.Algorithm != algorithm || record.Purpose != purpose || record.KeyID != keyID || record.BlobID != blobID {
		return nil, storage.Failure("store.corrupt", "legacy encrypted artifact is malformed or incompatible")
	}
	nonce, err := decodeBase64(record.Nonce, gcmNonceSize)
	if err != nil || len(nonce) != gcmNonceSize {
		clear(nonce)
		return nil, storage.Failure("store.corrupt", "legacy encrypted artifact nonce is invalid")
	}
	defer clear(nonce)
	ciphertext, err := decodeBase64(record.Ciphertext, maximum)
	if err != nil || len(ciphertext) < gcmTagSize {
		clear(ciphertext)
		return nil, storage.Failure("store.corrupt", "legacy encrypted artifact ciphertext is invalid")
	}
	defer clear(ciphertext)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, storage.Failure("store.crypto", "legacy secret encryption is unavailable")
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, storage.Failure("store.crypto", "legacy secret encryption is unavailable")
	}
	plain, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		clear(plain)
		return nil, storage.Failure("store.crypto", "legacy secret artifact authentication failed")
	}
	return plain, nil
}

func legacyMAC(key []byte, domain string, data []byte) string {
	derive := hmac.New(sha256.New, key)
	derive.Write([]byte(domain))
	macKey := derive.Sum(nil)
	defer clear(macKey)
	mac := hmac.New(sha256.New, macKey)
	mac.Write(data)
	return rawBase64.EncodeToString(mac.Sum(nil))
}

func verifyLegacyMAC(key []byte, domain string, data []byte, signature string) bool {
	actual, err := decodeBase64(signature, sha256.Size)
	defer clear(actual)
	expected, expectedErr := decodeBase64(legacyMAC(key, domain, data), sha256.Size)
	defer clear(expected)
	return err == nil && expectedErr == nil && hmac.Equal(actual, expected)
}

func upgradeMAC(record upgradeRecord, key []byte) string {
	record.MAC = ""
	data, _ := json.Marshal(record)
	return legacyMAC(key, "bootwright.secret.upgrade.mac-key.v2", data)
}

func validUpgrade(record upgradeRecord, contextID, backend string, source [sha256.Size]byte, key []byte) bool {
	if record.Version != formatVersion || record.ContextID != contextID || record.Backend != backend || record.SourceSelector != hex.EncodeToString(source[:]) || len(record.Attempts) == 0 || len(record.Attempts) > maxUpgradeAttempts {
		return false
	}
	seen := map[string]bool{}
	for _, attempt := range record.Attempts {
		if !validID(attempt.KeyID, "key-") || !validID(attempt.Generation, "gen-") || seen[attempt.KeyID] || seen[attempt.Generation] {
			return false
		}
		seen[attempt.KeyID], seen[attempt.Generation] = true, true
	}
	actual, err := decodeBase64(record.MAC, sha256.Size)
	defer clear(actual)
	expected, expectedErr := decodeBase64(upgradeMAC(record, key), sha256.Size)
	defer clear(expected)
	return err == nil && expectedErr == nil && hmac.Equal(actual, expected)
}

// An attempt's random generation determines exclusive output names without
// storing one journal entry per material part. It never determines a GCM nonce.
func upgradeBlobID(attempt upgradeAttempt, oldID string) string {
	digest := sha256.Sum256([]byte("bootwright.secret.upgrade.blob.v2\x00" + attempt.Generation + "\x00" + oldID))
	return "blob-" + hex.EncodeToString(digest[:16])
}

func unpublishedUpgradeArtifacts(ctx context.Context, area storage.Area, old *legacyState, journal upgradeRecord) ([]string, error) {
	allowed := map[string]bool{}
	for _, attempt := range journal.Attempts {
		allowed[keyPath(attempt.KeyID)] = true
		allowed[ledgerPath(attempt.KeyID)] = true
		for _, version := range old.index.Versions {
			for _, part := range version.Parts {
				allowed[partPath(upgradeBlobID(attempt, part.BlobID))] = true
			}
		}
	}
	paths := []string{}
	for _, directory := range []string{"keys", "parts"} {
		entries, err := area.Entries(ctx, directory)
		if err != nil {
			return nil, areaFailure(ctx, "store.corrupt", "interrupted secret upgrade artifacts cannot be enumerated", err)
		}
		for _, entry := range entries {
			path := directory + "/" + entry.Name
			if allowed[path] && !entry.Directory {
				paths = append(paths, path)
			}
		}
	}
	root, err := area.Entries(ctx, "")
	if err != nil {
		return nil, areaFailure(ctx, "store.corrupt", "interrupted secret upgrade publications cannot be enumerated", err)
	}
	for _, entry := range root {
		if entry.Directory || !validPending(entry.Name) {
			continue
		}
		attributed, err := attributedUpgradeTemporary(ctx, area, entry.Name, old, journal)
		if err != nil {
			return nil, err
		}
		if attributed {
			paths = append(paths, entry.Name)
		}
	}
	return paths, nil
}

func attributedUpgradeTemporary(ctx context.Context, area storage.Area, name string, old *legacyState, journal upgradeRecord) (bool, error) {
	data, exists, err := area.ReadMutable(ctx, name, storage.RecordMaximum)
	if err != nil || !exists {
		return false, areaFailure(ctx, "store.corrupt", "interrupted secret upgrade publication is unsafe", err)
	}
	defer clear(data)
	var intent upgradeRecord
	digest := sha256.Sum256(old.data)
	if decodeCanonical(data, selectorMaximum, 256, &intent) == nil && validUpgrade(intent, old.selector.ContextID, journal.Backend, digest, old.keys[old.index.ActiveKey]) {
		return true, nil
	}
	record, err := storage.DecodeRecord(data, old.selector.ContextID)
	if err != nil || record.Backend != journal.Backend {
		return false, nil
	}
	var wrapped metadataEnvelope
	if decodeMetadataPayload(record.Payload, &wrapped) != nil {
		return false, nil
	}
	for _, attempt := range journal.Attempts {
		if record.Generation != attempt.Generation || wrapped.KeyID != attempt.KeyID {
			continue
		}
		key, exists, err := area.ReadMutable(ctx, keyPath(attempt.KeyID), 32)
		if err != nil || !exists || len(key) != 32 {
			clear(key)
			return false, nil
		}
		plaintext, err := openMetadata(record, key)
		clear(key)
		if err != nil {
			return false, nil
		}
		var index indexRecord
		err = decodeCanonical(plaintext, indexMaximum, maxIndexItems, &index)
		clear(plaintext)
		index.FormatVersion, index.Algorithm, index.Selector = formatVersion, algorithm, record.Selector
		return err == nil && index.Legacy && index.ActiveKey == attempt.KeyID && validateIndex(index, record.Selector) == nil, nil
	}
	return false, nil
}

func verifyLegacyLayout(ctx context.Context, area storage.Area, old *legacyState, journal upgradeRecord) error {
	root, err := area.Entries(ctx, "")
	if err != nil {
		return areaFailure(ctx, "store.corrupt", "legacy secret layout cannot be enumerated", err)
	}
	directories := map[string]bool{"identities": false, "indexes": false, "keys": false, "ledgers": false, "parts": false}
	count := len(root)
	for _, entry := range root {
		if entry.Directory {
			if _, allowed := directories[entry.Name]; !allowed || directories[entry.Name] {
				return storage.Failure("store.corrupt", "legacy secret layout contains an unsupported directory")
			}
			directories[entry.Name] = true
		} else if entry.Name != legacySelectorPath && entry.Name != initializationPath && entry.Name != upgradePath && !validPending(entry.Name) {
			return storage.Failure("store.corrupt", "legacy secret layout contains an unsupported artifact")
		}
	}
	for _, present := range directories {
		if !present {
			return storage.Failure("store.corrupt", "legacy secret layout is incomplete")
		}
	}
	allowedNew := map[string]int64{}
	for _, attempt := range journal.Attempts {
		allowedNew[keyPath(attempt.KeyID)] = 32
		allowedNew[ledgerPath(attempt.KeyID)] = ledgerMaximum
		for _, version := range old.index.Versions {
			for _, part := range version.Parts {
				allowedNew[partPath(upgradeBlobID(attempt, part.BlobID))] = partMaximum
			}
		}
	}
	identities := map[string]bool{}
	for _, directory := range []string{"identities", "indexes", "keys", "ledgers", "parts"} {
		entries, err := area.Entries(ctx, directory)
		if err != nil {
			return areaFailure(ctx, "store.corrupt", "legacy secret artifacts cannot be enumerated", err)
		}
		count += len(entries)
		if count > maxPhysicalItems {
			return storage.Failure("store.limit", "legacy secret artifact count exceeds its limit")
		}
		for _, entry := range entries {
			if entry.Directory {
				return storage.Failure("store.corrupt", "legacy secret artifact type is invalid")
			}
			if maximum, allowed := allowedNew[directory+"/"+entry.Name]; allowed {
				if entry.Size < 0 || entry.Size > maximum {
					return storage.Failure("store.corrupt", "interrupted secret upgrade artifact exceeds its limit")
				}
				continue
			}
			if validPending(entry.Name) {
				continue
			}
			if directory == "identities" {
				id := strings.TrimSuffix(entry.Name, ".json")
				if !strings.HasSuffix(entry.Name, ".json") || !validID(id, "ver-") && !validID(id, "bind-") {
					return storage.Failure("store.corrupt", "legacy secret identity reservation name is invalid")
				}
				data, exists, err := area.ReadMutable(ctx, directory+"/"+entry.Name, selectorMaximum)
				var identity identityRecord
				if err != nil || !exists || decodeCanonical(data, selectorMaximum, 16, &identity) != nil || identity.FormatVersion != 1 || identity.ContextID != old.selector.ContextID || identity.ID != id {
					return areaFailure(ctx, "store.corrupt", "legacy secret identity reservation is invalid", err)
				}
				identities[id] = true
			} else if !legacyArtifactName(directory, entry.Name) {
				return storage.Failure("store.corrupt", "legacy secret artifact is unsupported or lacks upgrade attribution")
			}
		}
	}
	for _, version := range old.index.Versions {
		if !identities[version.ID] {
			return storage.Failure("store.corrupt", "legacy secret version identity reservation is missing")
		}
	}
	for _, binding := range old.index.Bindings {
		if !identities[binding.ID] {
			return storage.Failure("store.corrupt", "legacy secret binding identity reservation is missing")
		}
	}
	return nil
}

func legacyArtifactName(directory, name string) bool {
	prefix, suffix := "", ""
	switch directory {
	case "indexes":
		prefix, suffix = "gen-", ".bin"
	case "keys":
		prefix, suffix = "key-", ".bin"
	case "ledgers":
		prefix, suffix = "key-", ".json"
	case "parts":
		prefix, suffix = "blob-", ".bin"
	default:
		return false
	}
	return strings.HasSuffix(name, suffix) && validID(strings.TrimSuffix(name, suffix), prefix)
}

// Only an authenticated v2 manifest with Legacy set authorizes this cleanup.
// It deliberately does not require old keys: earlier cleanup may have removed
// them after the new metadata was made durable.
func legacyUpgradeArtifacts(ctx context.Context, area storage.Area) ([]string, error) {
	root, err := area.Entries(ctx, "")
	if err != nil {
		return nil, areaFailure(ctx, "store.corrupt", "retired secret layout cannot be enumerated", err)
	}
	present := map[string]bool{}
	paths := []string{}
	marker := false
	for _, entry := range root {
		if entry.Directory {
			present[entry.Name] = true
		} else if entry.Name == legacySelectorPath || entry.Name == initializationPath || entry.Name == upgradePath {
			if entry.Size <= 0 || entry.Size > selectorMaximum {
				return nil, storage.Failure("store.corrupt", "retired secret publication record is invalid")
			}
			if entry.Name == upgradePath {
				marker = true
			} else {
				paths = append(paths, entry.Name)
			}
		}
	}
	for _, directory := range []string{"indexes", "ledgers", "keys", "parts"} {
		if !present[directory] {
			continue
		}
		entries, err := area.Entries(ctx, directory)
		if err != nil {
			return nil, areaFailure(ctx, "store.corrupt", "retired secret artifacts cannot be enumerated", err)
		}
		for _, entry := range entries {
			if entry.Directory {
				return nil, storage.Failure("store.corrupt", "retired secret artifact type is invalid")
			}
			if legacyArtifactName(directory, entry.Name) || (directory == "indexes" || directory == "ledgers") && validPending(entry.Name) {
				paths = append(paths, directory+"/"+entry.Name)
			} else if directory == "indexes" || directory == "ledgers" {
				return nil, storage.Failure("store.corrupt", "retired secret directory contains an unsupported artifact")
			}
		}
		if directory == "indexes" || directory == "ledgers" {
			paths = append(paths, directory)
		}
	}
	if marker {
		paths = append(paths, upgradePath)
	}
	return paths, nil
}
