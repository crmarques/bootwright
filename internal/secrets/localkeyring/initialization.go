package localkeyring

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"slices"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

const initializationPath = "init.json"

func (i *Implementation) initialize(ctx context.Context, selected secretstore.Context, area secretstore.Area) (secretstore.StoreSession, error) {
	selectorData, selectorExists, err := area.ReadMutable(ctx, selectorPath, secretstore.RecordMaximum)
	if err != nil {
		return nil, areaFailure(ctx, "store.corrupt", "secret selector cannot be read safely", err)
	}
	if selectorExists {
		return i.openInitialized(ctx, selected, area, selectorData)
	}
	marker, err := i.initializationMarker(ctx, selected, area)
	if err != nil {
		return nil, err
	}
	if err := verifyInitializationArtifacts(ctx, area, selected, marker, true); err != nil {
		return nil, err
	}
	for _, name := range []string{"identities", "keys", "parts"} {
		if err := area.EnsureDirectory(ctx, name); err != nil {
			return nil, areaFailure(ctx, "store.conflict", "secret store directories could not be initialized", err)
		}
	}
	if err := verifyInitializationArtifacts(ctx, area, selected, marker, false); err != nil {
		return nil, err
	}
	for range 16 {
		session, retry, currentMarker, err := i.resumeInitialization(ctx, selected, area, selectorData, marker)
		if err != nil {
			return nil, err
		}
		if !retry {
			return session, nil
		}
		marker, err = i.retryInitialization(ctx, selected, area, currentMarker)
		if err != nil {
			return nil, err
		}
	}
	return nil, secretstore.Failure("store.limit", "secret initialization recovery exhausted its attempt limit")
}

func (i *Implementation) openInitialized(ctx context.Context, selected secretstore.Context, area secretstore.Area, selectorData []byte) (secretstore.StoreSession, error) {
	record, err := secretstore.DecodeRecord(selectorData, selected.Name)
	if err != nil || !validSelector(record.Selector, selected, i.Backend()) {
		return nil, secretstore.Failure("store.corrupt", "secret metadata is invalid or incompatible")
	}
	s, err := i.open(ctx, selected, area, record.Selector, nil)
	if err != nil {
		return nil, err
	}
	if err := s.collectArtifacts(ctx); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (i *Implementation) initializationMarker(ctx context.Context, selected secretstore.Context, area secretstore.Area) (initializationRecord, error) {
	markerData, markerExists, err := area.ReadMutable(ctx, initializationPath, selectorMaximum)
	if err != nil {
		return initializationRecord{}, areaFailure(ctx, "store.corrupt", "secret initialization record cannot be read safely", err)
	}
	root, err := area.Entries(ctx, "")
	if err != nil {
		return initializationRecord{}, areaFailure(ctx, "store.corrupt", "secret initialization state cannot be inspected safely", err)
	}
	if !validInitializationRoot(root, markerExists) {
		return initializationRecord{}, secretstore.Failure("store.corrupt", "nonempty secret store has no attributable initialization state")
	}
	var marker initializationRecord
	if markerExists {
		if decodeCanonical(markerData, selectorMaximum, 512, &marker) != nil || !validInitialization(marker, selected, i.Backend()) {
			return initializationRecord{}, secretstore.Failure("store.corrupt", "secret initialization record is invalid or incompatible")
		}
		if marker.MAC != "" {
			signingKey, err := initializationSigningKey(ctx, area, marker)
			if err != nil {
				return initializationRecord{}, err
			}
			clear(signingKey)
		}
		return marker, nil
	}
	pending, pendingExists, err := freshInitializationPending(ctx, area, selected, i.Backend(), root)
	if err != nil {
		return initializationRecord{}, err
	}
	if pendingExists {
		marker = pending
	} else {
		marker, err = i.newInitialization(ctx, selected, area, nil)
		if err != nil {
			return initializationRecord{}, err
		}
	}
	data, err := encodeCanonical(marker, selectorMaximum)
	if err != nil {
		return initializationRecord{}, err
	}
	outcome, replaceErr := area.Replace(ctx, initializationPath, data, markerData)
	if replaceErr != nil || outcome != secretstore.Committed {
		return initializationRecord{}, publicationFailure(ctx, outcome, replaceErr)
	}
	return marker, nil
}

func (i *Implementation) retryInitialization(ctx context.Context, selected secretstore.Context, area secretstore.Area, marker initializationRecord) (initializationRecord, error) {
	if len(marker.Attempts) >= 16 {
		return initializationRecord{}, secretstore.Failure("store.limit", "secret initialization recovery exhausted its attempt limit")
	}
	current, exists, readErr := area.ReadMutable(ctx, initializationPath, selectorMaximum)
	if readErr != nil || !exists {
		return initializationRecord{}, areaFailure(ctx, "store.corrupt", "secret initialization record changed during recovery", readErr)
	}
	var actual initializationRecord
	if decodeCanonical(current, selectorMaximum, 256, &actual) != nil || !reflectInitializationEqual(actual, marker) {
		return initializationRecord{}, secretstore.Failure("store.conflict", "secret initialization record changed during recovery")
	}
	next, err := i.newInitialization(ctx, selected, area, &marker)
	if err != nil {
		return initializationRecord{}, err
	}
	if marker.MAC != "" {
		signingKey, err := initializationSigningKey(ctx, area, marker)
		if err != nil {
			return initializationRecord{}, err
		}
		next = signInitialization(next, marker.MACKeyID, signingKey)
		clear(signingKey)
	}
	nextData, err := encodeCanonical(next, selectorMaximum)
	if err != nil {
		return initializationRecord{}, err
	}
	outcome, replaceErr := area.Replace(ctx, initializationPath, nextData, current)
	if replaceErr != nil || outcome != secretstore.Committed {
		return initializationRecord{}, publicationFailure(ctx, outcome, replaceErr)
	}
	if err := verifyInitializationArtifacts(ctx, area, selected, next, false); err != nil {
		return initializationRecord{}, err
	}
	return next, nil
}

func (i *Implementation) resumeInitialization(ctx context.Context, selected secretstore.Context, area secretstore.Area, selectorExpected []byte, marker initializationRecord) (secretstore.StoreSession, bool, initializationRecord, error) {
	if marker.MAC != "" {
		signingKey, err := initializationSigningKey(ctx, area, marker)
		if err != nil {
			return nil, false, marker, err
		}
		clear(signingKey)
	}
	attempt := marker.Attempts[len(marker.Attempts)-1]
	key, retry, err := i.initializationKey(ctx, area, attempt.KeyID)
	if err != nil || retry {
		return nil, retry, marker, err
	}
	keepKey := false
	defer func() {
		if !keepKey {
			clear(key)
		}
	}()
	if marker.MAC == "" {
		signed, err := authenticateInitialization(ctx, area, marker, attempt.KeyID, key)
		if err != nil {
			return nil, false, marker, err
		}
		marker = signed
	}
	ledgerEntry, err := namedEntry(ctx, area, "keys", attempt.KeyID+".usage.json")
	if err != nil {
		return nil, false, marker, err
	}
	ledger := sealLedger{}
	freshLedger := false
	if ledgerEntry == nil {
		ledgerData, err := encodeLedger(selected.Name, i.Backend(), key, attempt.KeyID, 1)
		if err != nil {
			return nil, false, marker, err
		}
		if err := area.WriteExclusive(ctx, ledgerPath(attempt.KeyID), ledgerData); err != nil {
			return nil, false, marker, areaFailure(ctx, "store.conflict", "secret seal reservation could not be stored", err)
		}
		ledger = sealLedger{FormatVersion: formatVersion, KeyID: attempt.KeyID, Seals: 1}
		freshLedger = true
	} else {
		data, exists, err := area.Read(ctx, ledgerPath(attempt.KeyID), ledgerMaximum)
		if err != nil || !exists {
			return nil, true, marker, nil
		}
		ledger, err = decodeLedger(data, selected.Name, i.Backend(), key, attempt.KeyID, 0)
		if err != nil {
			return nil, true, marker, nil
		}
	}
	selector := secretstore.Selector{SelectorVersion: secretstore.RecordVersion, Context: selected.Name, Backend: i.Backend(), Generation: attempt.Generation}
	index := indexRecord{FormatVersion: formatVersion, Algorithm: algorithm, Selector: selector, ActiveKey: attempt.KeyID, Keys: []storedKey{{ID: attempt.KeyID, Seals: ledger.Seals}}, Versions: []storedVersion{}, Current: []secretstore.Current{}, Bindings: []secretstore.Binding{}, Produced: []secretstore.Produced{}}
	if !freshLedger {
		seals, err := i.reserveInitializationSeal(ctx, selected, area, attempt.KeyID, key)
		if err != nil {
			return nil, false, marker, err
		}
		index.Keys[0].Seals = seals
	}
	plaintext, err := encodeCanonical(index, indexMaximum)
	if err != nil {
		return nil, false, marker, err
	}
	selectorData, err := sealMetadata(key, plaintext, selector, attempt.KeyID, i.random)
	clear(plaintext)
	if err != nil {
		return nil, false, marker, err
	}
	defer clear(selectorData)
	outcome, replaceErr := area.Replace(ctx, selectorPath, selectorData, selectorExpected)
	if replaceErr != nil || outcome != secretstore.Committed {
		return nil, false, marker, publicationFailure(ctx, outcome, replaceErr)
	}
	keepKey = true
	session := &session{implementation: i, context: selected, area: area, selector: selector, selectorData: slices.Clone(selectorData), index: index, keys: map[string][]byte{attempt.KeyID: key}}
	if err := session.collectArtifacts(ctx); err != nil {
		session.Close()
		return nil, false, marker, cleanupFailure(ctx, err)
	}
	return session, false, marker, nil
}

// initializationKey stores a fresh key for the attempt or recovers the one an
// interrupted attempt stored. A recorded key it cannot recover asks for a new
// attempt.
func (i *Implementation) initializationKey(ctx context.Context, area secretstore.Area, keyID string) ([]byte, bool, error) {
	keyEntry, err := namedEntry(ctx, area, "keys", keyID+".key")
	if err != nil {
		return nil, false, err
	}
	key := make([]byte, 32)
	if keyEntry == nil {
		if _, err := io.ReadFull(i.random, key); err != nil {
			clear(key)
			return nil, false, secretstore.Failure("store.crypto", "secret encryption randomness is unavailable")
		}
		if err := area.WriteExclusive(ctx, keyPath(keyID), key); err != nil {
			clear(key)
			return nil, false, areaFailure(ctx, "store.conflict", "secret encryption key could not be stored", err)
		}
		return key, false, nil
	}
	if keyEntry.Size != 32 {
		clear(key)
		return nil, true, nil
	}
	value, exists, err := area.ReadMutable(ctx, keyPath(keyID), 32)
	if err != nil || !exists || len(value) != 32 {
		clear(key)
		clear(value)
		return nil, true, nil
	}
	copy(key, value)
	clear(value)
	if err := area.SyncFile(ctx, keyPath(keyID)); err != nil {
		clear(key)
		return nil, false, areaFailure(ctx, "store.conflict", "recovered secret key durability could not be established", err)
	}
	return key, false, nil
}

func authenticateInitialization(ctx context.Context, area secretstore.Area, marker initializationRecord, keyID string, key []byte) (initializationRecord, error) {
	current, exists, err := area.ReadMutable(ctx, initializationPath, selectorMaximum)
	if err != nil || !exists {
		return marker, areaFailure(ctx, "store.corrupt", "secret initialization record changed before authentication", err)
	}
	var actual initializationRecord
	if decodeCanonical(current, selectorMaximum, 256, &actual) != nil || !reflectInitializationEqual(actual, marker) {
		return marker, secretstore.Failure("store.conflict", "secret initialization record changed before authentication")
	}
	signed := signInitialization(marker, keyID, key)
	data, err := encodeCanonical(signed, selectorMaximum)
	if err != nil {
		return marker, err
	}
	outcome, replaceErr := area.Replace(ctx, initializationPath, data, current)
	if replaceErr != nil || outcome != secretstore.Committed {
		return marker, publicationFailure(ctx, outcome, replaceErr)
	}
	return signed, nil
}

func (i *Implementation) reserveInitializationSeal(ctx context.Context, selected secretstore.Context, area secretstore.Area, keyID string, key []byte) (uint64, error) {
	ledgerData, exists, err := area.ReadMutable(ctx, ledgerPath(keyID), ledgerMaximum)
	if err != nil || !exists {
		return 0, areaFailure(ctx, "store.corrupt", "secret initialization ledger cannot be reserved", err)
	}
	ledger, err := decodeLedger(ledgerData, selected.Name, i.Backend(), key, keyID, 0)
	if err != nil || ledger.Seals >= maxSeals {
		return 0, secretstore.Failure("store.limit", "secret initialization seal limit is exhausted")
	}
	ledger.Seals++
	nextLedger, err := encodeLedger(selected.Name, i.Backend(), key, keyID, ledger.Seals)
	if err != nil {
		return 0, err
	}
	outcome, replaceErr := area.Replace(ctx, ledgerPath(keyID), nextLedger, ledgerData)
	if replaceErr != nil || outcome != secretstore.Committed {
		return 0, publicationFailure(ctx, outcome, replaceErr)
	}
	return ledger.Seals, nil
}

func (i *Implementation) newInitialization(ctx context.Context, selected secretstore.Context, area secretstore.Area, prior *initializationRecord) (initializationRecord, error) {
	if err := ctx.Err(); err != nil {
		return initializationRecord{}, err
	}
	keys, keyErr := optionalEntryNames(ctx, area, "keys")
	ledgers, ledgerErr := optionalEntryNames(ctx, area, "keys")
	if prior != nil && (keyErr != nil || ledgerErr != nil) {
		return initializationRecord{}, secretstore.Failure("store.corrupt", "secret initialization artifacts cannot be enumerated")
	}
	attempts := []initializationAttempt{}
	if prior != nil {
		attempts = slices.Clone(prior.Attempts)
	}
	keyID, err := i.uniqueID("key-", func(id string) bool { return keys[id+".key"] || ledgers[id+".usage.json"] })
	if err != nil {
		return initializationRecord{}, err
	}
	generation, err := i.uniqueID("gen-", func(id string) bool {
		for _, attempt := range attempts {
			if attempt.Generation == id {
				return true
			}
		}
		return false
	})
	if err != nil {
		return initializationRecord{}, err
	}
	attempts = append(attempts, initializationAttempt{KeyID: keyID, Generation: generation})
	return initializationRecord{FormatVersion: formatVersion, Context: selected.Name, Selection: i.Backend(), Attempts: attempts}, nil
}

func validInitialization(record initializationRecord, selected secretstore.Context, selection string) bool {
	if record.FormatVersion != formatVersion || record.Context != selected.Name || record.Selection != selection || len(record.Attempts) == 0 || len(record.Attempts) > 16 || (record.MAC == "") != (record.MACKeyID == "") {
		return false
	}
	ids := map[string]bool{}
	macKey := record.MACKeyID == ""
	for _, attempt := range record.Attempts {
		if !validID(attempt.KeyID, "key-") || !validID(attempt.Generation, "gen-") || ids[attempt.KeyID] || ids[attempt.Generation] {
			return false
		}
		ids[attempt.KeyID], ids[attempt.Generation] = true, true
		macKey = macKey || attempt.KeyID == record.MACKeyID
	}
	return macKey
}

func reflectInitializationEqual(a, b initializationRecord) bool { return reflect.DeepEqual(a, b) }

func validInitializationRoot(entries []secretstore.Entry, marker bool) bool {
	for _, entry := range entries {
		if entry.Directory {
			if !marker || !slices.Contains([]string{"identities", "keys", "parts"}, entry.Name) {
				return false
			}
			continue
		}
		if entry.Name == initializationPath {
			if !marker || entry.Size <= 0 || entry.Size > selectorMaximum {
				return false
			}
			continue
		}
		if !validPending(entry.Name) {
			return false
		}
	}
	return marker || len(entries) == 0 || onlyPending(entries)
}

func freshInitializationPending(ctx context.Context, area secretstore.Area, selected secretstore.Context, selection string, entries []secretstore.Entry) (initializationRecord, bool, error) {
	var recovered initializationRecord
	found := false
	for _, entry := range entries {
		if entry.Directory || !validPending(entry.Name) {
			return initializationRecord{}, false, secretstore.Failure("store.corrupt", "unattributed secret initialization state is present")
		}
		data, exists, err := area.Read(ctx, entry.Name, selectorMaximum)
		if err != nil || !exists {
			return initializationRecord{}, false, areaFailure(ctx, "store.corrupt", "pending secret initialization record is unsafe", err)
		}
		if interruptedStage(data) {
			continue
		}
		var candidate initializationRecord
		if decodeCanonical(data, selectorMaximum, 512, &candidate) != nil || !validInitialization(candidate, selected, selection) || candidate.MAC != "" || len(candidate.Attempts) != 1 {
			return initializationRecord{}, false, secretstore.Failure("store.corrupt", "pending secret initialization record is not attributable")
		}
		if found && !reflectInitializationEqual(recovered, candidate) {
			return initializationRecord{}, false, secretstore.Failure("store.corrupt", "pending secret initialization records disagree")
		}
		recovered, found = candidate, true
	}
	return recovered, found, nil
}

func verifyInitializationArtifacts(ctx context.Context, area secretstore.Area, selected secretstore.Context, marker initializationRecord, allowMissing bool) error {
	root, err := area.Entries(ctx, "")
	if err != nil {
		return areaFailure(ctx, "store.corrupt", "secret initialization layout cannot be enumerated", err)
	}
	present := map[string]bool{}
	for _, entry := range root {
		if entry.Directory {
			present[entry.Name] = true
			if !slices.Contains([]string{"identities", "keys", "parts"}, entry.Name) {
				return secretstore.Failure("store.corrupt", "secret initialization layout contains an unsupported directory")
			}
			continue
		}
		if entry.Name == initializationPath {
			continue
		}
		if !validPending(entry.Name) {
			return secretstore.Failure("store.corrupt", "secret initialization layout contains an unsupported artifact")
		}
		data, exists, err := area.Read(ctx, entry.Name, selectorMaximum)
		if err == nil && exists && interruptedStage(data) {
			continue
		}
		if err != nil || !exists || !validRootInitializationPending(ctx, area, data, selected, marker) {
			return secretstore.Failure("store.corrupt", "pending secret initialization publication is not attributable")
		}
	}
	allowedKeys := map[string]bool{}
	for _, attempt := range marker.Attempts {
		allowedKeys[attempt.KeyID+".key"] = true
		allowedKeys[attempt.KeyID+".usage.json"] = true
	}
	for _, directory := range []string{"identities", "parts"} {
		if !present[directory] {
			if allowMissing {
				continue
			}
			return secretstore.Failure("store.corrupt", "secret initialization layout is incomplete")
		}
		entries, err := area.Entries(ctx, directory)
		if err != nil {
			return areaFailure(ctx, "store.corrupt", "secret initialization artifacts cannot be enumerated", err)
		}
		if len(entries) != 0 {
			return secretstore.Failure("store.corrupt", "published-use evidence forbids secret initialization recovery")
		}
	}
	for _, table := range []struct {
		name    string
		allowed map[string]bool
	}{
		{name: "keys", allowed: allowedKeys},
	} {
		if !present[table.name] {
			if allowMissing {
				continue
			}
			return secretstore.Failure("store.corrupt", "secret initialization layout is incomplete")
		}
		entries, err := area.Entries(ctx, table.name)
		if err != nil {
			return areaFailure(ctx, "store.corrupt", "secret initialization artifacts cannot be enumerated", err)
		}
		for _, entry := range entries {
			if entry.Directory {
				return secretstore.Failure("store.corrupt", "secret initialization artifact type is invalid")
			}
			if table.allowed[entry.Name] {
				continue
			}
			if table.name == "keys" && validPending(entry.Name) {
				data, exists, err := area.Read(ctx, "keys/"+entry.Name, ledgerMaximum)
				if err == nil && exists && interruptedStage(data) {
					continue
				}
			}
			if table.name != "keys" || !validPending(entry.Name) || !validInitializationLedgerPending(ctx, area, entry.Name, selected, marker) {
				return secretstore.Failure("store.corrupt", "secret initialization contains non-attributable artifacts")
			}
		}
	}
	return nil
}

func validRootInitializationPending(ctx context.Context, area secretstore.Area, data []byte, selected secretstore.Context, marker initializationRecord) bool {
	var candidate initializationRecord
	if decodeCanonical(data, selectorMaximum, 512, &candidate) == nil && validInitialization(candidate, selected, marker.Selection) {
		candidatePrefix := initializationAttemptsPrefix(candidate.Attempts, marker.Attempts)
		markerPrefix := initializationAttemptsPrefix(marker.Attempts, candidate.Attempts)
		if !candidatePrefix && !markerPrefix {
			return false
		}
		if candidate.MAC == "" {
			return candidatePrefix || marker.MAC == "" && len(candidate.Attempts) <= len(marker.Attempts)+1
		}
		if marker.MAC != "" && candidate.MACKeyID != marker.MACKeyID {
			return false
		}
		key, exists, err := area.Read(ctx, keyPath(candidate.MACKeyID), 32)
		if err != nil || !exists || len(key) != 32 {
			clear(key)
			return false
		}
		valid := verifyInitializationMAC(candidate, key)
		clear(key)
		return valid
	}
	record, err := secretstore.DecodeRecord(data, selected.Name)
	if err != nil || !validSelector(record.Selector, selected, marker.Selection) {
		return false
	}
	for _, attempt := range marker.Attempts {
		if record.Generation != attempt.Generation {
			continue
		}
		key, err := readKey(ctx, area, attempt.KeyID)
		if err != nil {
			return false
		}
		plaintext, err := openMetadata(record, key)
		clear(key)
		if err != nil {
			return false
		}
		var index indexRecord
		decodeErr := decodeCanonical(plaintext, indexMaximum, maxIndexItems, &index)
		clear(plaintext)
		index.FormatVersion, index.Algorithm, index.Selector = formatVersion, algorithm, record.Selector
		return decodeErr == nil && validateIndex(index, record.Selector) == nil && index.ActiveKey == attempt.KeyID && len(index.Versions) == 0 && len(index.Current) == 0 && len(index.Bindings) == 0 && len(index.Keys) == 1
	}
	return false
}

func interruptedStage(data []byte) bool { return !json.Valid(data) }

func initializationAttemptsPrefix(prefix, complete []initializationAttempt) bool {
	return len(prefix) <= len(complete) && slices.Equal(prefix, complete[:len(prefix)])
}

func validInitializationLedgerPending(ctx context.Context, area secretstore.Area, name string, selected secretstore.Context, marker initializationRecord) bool {
	data, exists, err := area.Read(ctx, "keys/"+name, ledgerMaximum)
	if err != nil || !exists {
		return false
	}
	var candidate sealLedger
	if decodeCanonical(data, ledgerMaximum, 24, &candidate) != nil {
		return false
	}
	allowed := false
	for _, attempt := range marker.Attempts {
		allowed = allowed || attempt.KeyID == candidate.KeyID
	}
	if !allowed {
		return false
	}
	key, exists, err := area.Read(ctx, keyPath(candidate.KeyID), 32)
	if err != nil || !exists || len(key) != 32 {
		clear(key)
		return false
	}
	_, err = decodeLedger(data, selected.Name, marker.Selection, key, candidate.KeyID, 0)
	clear(key)
	return err == nil
}

func initializationSigningKey(ctx context.Context, area secretstore.Area, marker initializationRecord) ([]byte, error) {
	key, exists, err := area.Read(ctx, keyPath(marker.MACKeyID), 32)
	if err != nil || !exists || len(key) != 32 {
		clear(key)
		return nil, secretstore.Failure("store.corrupt", "secret initialization authentication key is missing or unsafe")
	}
	if !verifyInitializationMAC(marker, key) {
		clear(key)
		return nil, secretstore.Failure("store.corrupt", "secret initialization authentication is invalid")
	}
	return key, nil
}

func onlyPending(entries []secretstore.Entry) bool {
	for _, entry := range entries {
		if entry.Directory || !validPending(entry.Name) {
			return false
		}
	}
	return true
}

func optionalEntryNames(ctx context.Context, area secretstore.Area, directory string) (map[string]bool, error) {
	entries, err := area.Entries(ctx, directory)
	if err != nil {
		return map[string]bool{}, err
	}
	result := make(map[string]bool, len(entries))
	for _, entry := range entries {
		result[entry.Name] = true
	}
	return result, nil
}

func namedEntry(ctx context.Context, area secretstore.Area, directory, name string) (*secretstore.Entry, error) {
	entries, err := area.Entries(ctx, directory)
	if err != nil {
		return nil, areaFailure(ctx, "store.corrupt", "secret initialization artifacts cannot be enumerated", err)
	}
	for i := range entries {
		if entries[i].Name == name {
			entry := entries[i]
			return &entry, nil
		}
	}
	return nil, nil
}
