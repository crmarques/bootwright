package localstore

import (
	"context"
	"io"
	"reflect"
	"slices"

	"github.com/crmarques/bootwright/internal/secrets/storage"
)

const initializationPath = "init.json"

func (i *Implementation) initialize(ctx context.Context, selected storage.Context, area storage.Area) (storage.StoreSession, error) {
	selectorData, selectorExists, err := area.ReadMutable(ctx, selectorPath, selectorMaximum)
	if err != nil {
		return nil, areaFailure(ctx, "store.corrupt", "secret selector cannot be read safely", err)
	}
	if selectorExists {
		var selector storage.Selector
		if decodeCanonical(selectorData, selectorMaximum, 64, &selector) != nil || !validSelector(selector, selected, i.Selection()) {
			return nil, storage.Failure("store.corrupt", "secret selector is invalid or incompatible")
		}
		return i.Open(ctx, selected, area, selector, nil)
	}
	markerData, markerExists, err := area.ReadMutable(ctx, initializationPath, selectorMaximum)
	if err != nil {
		return nil, areaFailure(ctx, "store.corrupt", "secret initialization record cannot be read safely", err)
	}
	root, err := area.Entries(ctx, "")
	if err != nil {
		return nil, areaFailure(ctx, "store.corrupt", "secret initialization state cannot be inspected safely", err)
	}
	if !validInitializationRoot(root, markerExists) {
		return nil, storage.Failure("store.corrupt", "nonempty secret store has no attributable initialization state")
	}
	var marker initializationRecord
	if markerExists {
		if decodeCanonical(markerData, selectorMaximum, 512, &marker) != nil || !validInitialization(marker, selected, i.Selection()) {
			return nil, storage.Failure("store.corrupt", "secret initialization record is invalid or incompatible")
		}
		if marker.MAC != "" {
			signingKey, err := initializationSigningKey(ctx, area, marker)
			if err != nil {
				return nil, err
			}
			clear(signingKey)
		}
	} else {
		pending, pendingExists, err := freshInitializationPending(ctx, area, selected, i.Selection(), root)
		if err != nil {
			return nil, err
		}
		if pendingExists {
			marker = pending
		} else {
			marker, err = i.newInitialization(ctx, selected, area, nil)
			if err != nil {
				return nil, err
			}
		}
		data, err := encodeCanonical(marker, selectorMaximum)
		if err != nil {
			return nil, err
		}
		outcome, replaceErr := area.Replace(ctx, initializationPath, data, markerData)
		if replaceErr != nil || outcome != storage.Committed {
			return nil, publicationFailure(ctx, outcome, replaceErr)
		}
		markerData = data
	}
	if err := verifyInitializationArtifacts(ctx, area, selected, marker, true); err != nil {
		return nil, err
	}
	for _, name := range []string{"identities", "indexes", "keys", "ledgers", "parts"} {
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
		marker = currentMarker
		if !retry {
			return session, nil
		}
		if len(marker.Attempts) >= 16 {
			return nil, storage.Failure("store.limit", "secret initialization recovery exhausted its attempt limit")
		}
		current, exists, readErr := area.ReadMutable(ctx, initializationPath, selectorMaximum)
		if readErr != nil || !exists {
			return nil, areaFailure(ctx, "store.corrupt", "secret initialization record changed during recovery", readErr)
		}
		var actual initializationRecord
		if decodeCanonical(current, selectorMaximum, 256, &actual) != nil || !reflectInitializationEqual(actual, marker) {
			return nil, storage.Failure("store.conflict", "secret initialization record changed during recovery")
		}
		next, err := i.newInitialization(ctx, selected, area, &marker)
		if err != nil {
			return nil, err
		}
		if marker.MAC != "" {
			signingKey, err := initializationSigningKey(ctx, area, marker)
			if err != nil {
				return nil, err
			}
			next = signInitialization(next, marker.MACKeyID, signingKey)
			clear(signingKey)
		}
		nextData, err := encodeCanonical(next, selectorMaximum)
		if err != nil {
			return nil, err
		}
		outcome, replaceErr := area.Replace(ctx, initializationPath, nextData, current)
		if replaceErr != nil || outcome != storage.Committed {
			return nil, publicationFailure(ctx, outcome, replaceErr)
		}
		marker, markerData = next, nextData
		if err := verifyInitializationArtifacts(ctx, area, selected, marker, false); err != nil {
			return nil, err
		}
	}
	return nil, storage.Failure("store.limit", "secret initialization recovery exhausted its attempt limit")
}

func (i *Implementation) resumeInitialization(ctx context.Context, selected storage.Context, area storage.Area, selectorExpected []byte, marker initializationRecord) (storage.StoreSession, bool, initializationRecord, error) {
	if marker.MAC != "" {
		signingKey, err := initializationSigningKey(ctx, area, marker)
		if err != nil {
			return nil, false, marker, err
		}
		clear(signingKey)
	}
	attempt := marker.Attempts[len(marker.Attempts)-1]
	keyEntry, err := namedEntry(ctx, area, "keys", attempt.KeyID+".bin")
	if err != nil {
		return nil, false, marker, err
	}
	key := make([]byte, 32)
	if keyEntry == nil {
		if _, err := io.ReadFull(i.random, key); err != nil {
			clear(key)
			return nil, false, marker, storage.Failure("store.crypto", "secret encryption randomness is unavailable")
		}
		if err := area.WriteExclusive(ctx, "keys/"+attempt.KeyID+".bin", key); err != nil {
			clear(key)
			return nil, false, marker, areaFailure(ctx, "store.conflict", "secret encryption key could not be stored", err)
		}
	} else {
		if keyEntry.Size != 32 {
			clear(key)
			return nil, true, marker, nil
		}
		value, exists, err := area.Read(ctx, "keys/"+attempt.KeyID+".bin", 32)
		if err != nil || !exists || len(value) != 32 {
			clear(key)
			clear(value)
			return nil, true, marker, nil
		}
		copy(key, value)
		clear(value)
	}
	keepKey := false
	defer func() {
		if !keepKey {
			clear(key)
		}
	}()
	if marker.MAC == "" {
		current, exists, err := area.ReadMutable(ctx, initializationPath, selectorMaximum)
		if err != nil || !exists {
			return nil, false, marker, areaFailure(ctx, "store.corrupt", "secret initialization record changed before authentication", err)
		}
		var actual initializationRecord
		if decodeCanonical(current, selectorMaximum, 256, &actual) != nil || !reflectInitializationEqual(actual, marker) {
			return nil, false, marker, storage.Failure("store.conflict", "secret initialization record changed before authentication")
		}
		signed := signInitialization(marker, attempt.KeyID, key)
		data, err := encodeCanonical(signed, selectorMaximum)
		if err != nil {
			return nil, false, marker, err
		}
		outcome, replaceErr := area.Replace(ctx, initializationPath, data, current)
		if replaceErr != nil || outcome != storage.Committed {
			return nil, false, marker, publicationFailure(ctx, outcome, replaceErr)
		}
		marker = signed
	}
	ledgerEntry, err := namedEntry(ctx, area, "ledgers", attempt.KeyID+".json")
	if err != nil {
		return nil, false, marker, err
	}
	ledger := sealLedger{}
	freshLedger := false
	if ledgerEntry == nil {
		ledgerData, err := encodeLedger(selected.ID, i.Selection(), key, attempt.KeyID, 1)
		if err != nil {
			return nil, false, marker, err
		}
		if err := area.WriteExclusive(ctx, "ledgers/"+attempt.KeyID+".json", ledgerData); err != nil {
			return nil, false, marker, areaFailure(ctx, "store.conflict", "secret seal reservation could not be stored", err)
		}
		ledger = sealLedger{FormatVersion: formatVersion, KeyID: attempt.KeyID, Seals: 1}
		freshLedger = true
	} else {
		data, exists, err := area.Read(ctx, "ledgers/"+attempt.KeyID+".json", ledgerMaximum)
		if err != nil || !exists {
			return nil, true, marker, nil
		}
		ledger, err = decodeLedger(data, selected.ID, i.Selection(), key, attempt.KeyID, 0)
		if err != nil {
			return nil, true, marker, nil
		}
	}
	selector := storage.Selector{SelectorVersion: formatVersion, ContextID: selected.ID, Selection: i.Selection(), Generation: attempt.Generation}
	index := indexRecord{FormatVersion: formatVersion, Algorithm: algorithm, Selector: selector, ActiveKey: attempt.KeyID, Keys: []storage.Key{{ID: attempt.KeyID, State: "active", Seals: ledger.Seals}}, Versions: []storedVersion{}, Current: []storage.Current{}, Bindings: []storage.Binding{}}
	indexEntry, err := namedEntry(ctx, area, "indexes", attempt.Generation+".bin")
	if err != nil {
		return nil, false, marker, err
	}
	if indexEntry == nil {
		if !freshLedger {
			ledgerData, exists, err := area.ReadMutable(ctx, "ledgers/"+attempt.KeyID+".json", ledgerMaximum)
			if err != nil || !exists {
				return nil, false, marker, areaFailure(ctx, "store.corrupt", "secret initialization ledger cannot be reserved", err)
			}
			ledger, err = decodeLedger(ledgerData, selected.ID, i.Selection(), key, attempt.KeyID, 0)
			if err != nil || ledger.Seals >= maxSeals {
				return nil, false, marker, storage.Failure("store.limit", "secret initialization seal limit is exhausted")
			}
			ledger.Seals++
			nextLedger, err := encodeLedger(selected.ID, i.Selection(), key, attempt.KeyID, ledger.Seals)
			if err != nil {
				return nil, false, marker, err
			}
			outcome, replaceErr := area.Replace(ctx, "ledgers/"+attempt.KeyID+".json", nextLedger, ledgerData)
			if replaceErr != nil || outcome != storage.Committed {
				return nil, false, marker, publicationFailure(ctx, outcome, replaceErr)
			}
			index.Keys[0].Seals = ledger.Seals
		}
		plaintext, err := encodeCanonical(index, indexMaximum)
		if err != nil {
			return nil, false, marker, err
		}
		sealed, err := seal(key, plaintext, indexAAD(selected.ID, selector, attempt.KeyID), "index", attempt.KeyID, attempt.Generation, i.random, indexMaximum)
		clear(plaintext)
		if err != nil {
			return nil, false, marker, err
		}
		err = area.WriteExclusive(ctx, "indexes/"+attempt.Generation+".bin", sealed)
		clear(sealed)
		if err != nil {
			return nil, false, marker, areaFailure(ctx, "store.conflict", "encrypted secret index could not be stored", err)
		}
	} else {
		data, exists, err := area.Read(ctx, "indexes/"+attempt.Generation+".bin", indexMaximum)
		if err != nil || !exists {
			return nil, true, marker, nil
		}
		plaintext, err := openEnvelope(data, key, indexAAD(selected.ID, selector, attempt.KeyID), "index", attempt.KeyID, attempt.Generation, indexMaximum)
		clear(data)
		if err != nil {
			return nil, true, marker, nil
		}
		var persisted indexRecord
		decodeErr := decodeCanonical(plaintext, indexMaximum, maxIndexItems, &persisted)
		clear(plaintext)
		if decodeErr != nil || validateIndex(persisted, selector) != nil || persisted.ActiveKey != attempt.KeyID || len(persisted.Versions) != 0 || len(persisted.Current) != 0 || len(persisted.Bindings) != 0 || len(persisted.Keys) != 1 || persisted.Keys[0].Seals > ledger.Seals {
			return nil, true, marker, nil
		}
		index = persisted
		index.Keys[0].Seals = ledger.Seals
	}
	selectorData, err := encodeCanonical(selector, selectorMaximum)
	if err != nil {
		return nil, false, marker, err
	}
	outcome, replaceErr := area.Replace(ctx, selectorPath, selectorData, selectorExpected)
	if replaceErr != nil || outcome != storage.Committed {
		return nil, false, marker, publicationFailure(ctx, outcome, replaceErr)
	}
	keepKey = true
	return &session{implementation: i, context: selected, area: area, selector: selector, selectorData: slices.Clone(selectorData), index: index, keys: map[string][]byte{attempt.KeyID: key}}, false, marker, nil
}

func (i *Implementation) newInitialization(ctx context.Context, selected storage.Context, area storage.Area, prior *initializationRecord) (initializationRecord, error) {
	if err := ctx.Err(); err != nil {
		return initializationRecord{}, err
	}
	keys, keyErr := optionalEntryNames(ctx, area, "keys")
	ledgers, ledgerErr := optionalEntryNames(ctx, area, "ledgers")
	indexes, indexErr := optionalEntryNames(ctx, area, "indexes")
	if prior != nil && (keyErr != nil || ledgerErr != nil || indexErr != nil) {
		return initializationRecord{}, storage.Failure("store.corrupt", "secret initialization artifacts cannot be enumerated")
	}
	usedAttempts := map[string]bool{}
	attempts := []initializationAttempt{}
	if prior != nil {
		attempts = slices.Clone(prior.Attempts)
		for _, attempt := range attempts {
			usedAttempts[attempt.AttemptID] = true
		}
	}
	attemptID, err := i.uniqueID("init-", func(id string) bool { return usedAttempts[id] })
	if err != nil {
		return initializationRecord{}, err
	}
	keyID, err := i.uniqueID("key-", func(id string) bool { return keys[id+".bin"] || ledgers[id+".json"] })
	if err != nil {
		return initializationRecord{}, err
	}
	generation, err := i.uniqueID("gen-", func(id string) bool { return indexes[id+".bin"] })
	if err != nil {
		return initializationRecord{}, err
	}
	attempts = append(attempts, initializationAttempt{AttemptID: attemptID, KeyID: keyID, Generation: generation})
	return initializationRecord{FormatVersion: formatVersion, ContextID: selected.ID, Selection: i.Selection(), Attempts: attempts}, nil
}

func validInitialization(record initializationRecord, selected storage.Context, selection storage.Selection) bool {
	if record.FormatVersion != formatVersion || record.ContextID != selected.ID || record.Selection != selection || len(record.Attempts) == 0 || len(record.Attempts) > 16 || (record.MAC == "") != (record.MACKeyID == "") {
		return false
	}
	ids := map[string]bool{}
	macKey := record.MACKeyID == ""
	for _, attempt := range record.Attempts {
		if !validID(attempt.AttemptID, "init-") || !validID(attempt.KeyID, "key-") || !validID(attempt.Generation, "gen-") || ids[attempt.AttemptID] || ids[attempt.KeyID] || ids[attempt.Generation] {
			return false
		}
		ids[attempt.AttemptID], ids[attempt.KeyID], ids[attempt.Generation] = true, true, true
		macKey = macKey || attempt.KeyID == record.MACKeyID
	}
	return macKey
}

func reflectInitializationEqual(a, b initializationRecord) bool { return reflect.DeepEqual(a, b) }

func validInitializationRoot(entries []storage.Entry, marker bool) bool {
	for _, entry := range entries {
		if entry.Directory {
			if !marker || !slices.Contains([]string{"identities", "indexes", "keys", "ledgers", "parts"}, entry.Name) {
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

func freshInitializationPending(ctx context.Context, area storage.Area, selected storage.Context, selection storage.Selection, entries []storage.Entry) (initializationRecord, bool, error) {
	var recovered initializationRecord
	found := false
	for _, entry := range entries {
		if entry.Directory || !validPending(entry.Name) {
			return initializationRecord{}, false, storage.Failure("store.corrupt", "unattributed secret initialization state is present")
		}
		data, exists, err := area.Read(ctx, entry.Name, selectorMaximum)
		if err != nil || !exists {
			return initializationRecord{}, false, areaFailure(ctx, "store.corrupt", "pending secret initialization record is unsafe", err)
		}
		var candidate initializationRecord
		if decodeCanonical(data, selectorMaximum, 512, &candidate) != nil || !validInitialization(candidate, selected, selection) || candidate.MAC != "" || len(candidate.Attempts) != 1 {
			return initializationRecord{}, false, storage.Failure("store.corrupt", "pending secret initialization record is not attributable")
		}
		if found && !reflectInitializationEqual(recovered, candidate) {
			return initializationRecord{}, false, storage.Failure("store.corrupt", "pending secret initialization records disagree")
		}
		recovered, found = candidate, true
	}
	return recovered, found, nil
}

func verifyInitializationArtifacts(ctx context.Context, area storage.Area, selected storage.Context, marker initializationRecord, allowMissing bool) error {
	root, err := area.Entries(ctx, "")
	if err != nil {
		return areaFailure(ctx, "store.corrupt", "secret initialization layout cannot be enumerated", err)
	}
	present := map[string]bool{}
	for _, entry := range root {
		if entry.Directory {
			present[entry.Name] = true
			if !slices.Contains([]string{"identities", "indexes", "keys", "ledgers", "parts"}, entry.Name) {
				return storage.Failure("store.corrupt", "secret initialization layout contains an unsupported directory")
			}
			continue
		}
		if entry.Name == initializationPath {
			continue
		}
		if !validPending(entry.Name) {
			return storage.Failure("store.corrupt", "secret initialization layout contains an unsupported artifact")
		}
		data, exists, err := area.Read(ctx, entry.Name, selectorMaximum)
		if err != nil || !exists || !validRootInitializationPending(ctx, area, data, selected, marker) {
			return storage.Failure("store.corrupt", "pending secret initialization publication is not attributable")
		}
	}
	allowedKeys, allowedLedgers, allowedIndexes := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, attempt := range marker.Attempts {
		allowedKeys[attempt.KeyID+".bin"] = true
		allowedLedgers[attempt.KeyID+".json"] = true
		allowedIndexes[attempt.Generation+".bin"] = true
	}
	for _, directory := range []string{"identities", "parts"} {
		if !present[directory] {
			if allowMissing {
				continue
			}
			return storage.Failure("store.corrupt", "secret initialization layout is incomplete")
		}
		entries, err := area.Entries(ctx, directory)
		if err != nil {
			return areaFailure(ctx, "store.corrupt", "secret initialization artifacts cannot be enumerated", err)
		}
		if len(entries) != 0 {
			return storage.Failure("store.corrupt", "published-use evidence forbids secret initialization recovery")
		}
	}
	for _, table := range []struct {
		name    string
		allowed map[string]bool
	}{
		{name: "keys", allowed: allowedKeys},
		{name: "indexes", allowed: allowedIndexes},
		{name: "ledgers", allowed: allowedLedgers},
	} {
		if !present[table.name] {
			if allowMissing {
				continue
			}
			return storage.Failure("store.corrupt", "secret initialization layout is incomplete")
		}
		entries, err := area.Entries(ctx, table.name)
		if err != nil {
			return areaFailure(ctx, "store.corrupt", "secret initialization artifacts cannot be enumerated", err)
		}
		for _, entry := range entries {
			if entry.Directory {
				return storage.Failure("store.corrupt", "secret initialization artifact type is invalid")
			}
			if table.allowed[entry.Name] {
				continue
			}
			if table.name != "ledgers" || !validPending(entry.Name) || !validInitializationLedgerPending(ctx, area, entry.Name, selected, marker) {
				return storage.Failure("store.corrupt", "secret initialization contains non-attributable artifacts")
			}
		}
	}
	return nil
}

func validRootInitializationPending(ctx context.Context, area storage.Area, data []byte, selected storage.Context, marker initializationRecord) bool {
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
		key, exists, err := area.Read(ctx, "keys/"+candidate.MACKeyID+".bin", 32)
		if err != nil || !exists || len(key) != 32 {
			clear(key)
			return false
		}
		valid := verifyInitializationMAC(candidate, key)
		clear(key)
		return valid
	}
	var selector storage.Selector
	if decodeCanonical(data, selectorMaximum, 64, &selector) != nil || selector.SelectorVersion != formatVersion || selector.ContextID != selected.ID || selector.Selection != marker.Selection {
		return false
	}
	for _, attempt := range marker.Attempts {
		if selector.Generation == attempt.Generation {
			return true
		}
	}
	return false
}

func initializationAttemptsPrefix(prefix, complete []initializationAttempt) bool {
	return len(prefix) <= len(complete) && slices.Equal(prefix, complete[:len(prefix)])
}

func validInitializationLedgerPending(ctx context.Context, area storage.Area, name string, selected storage.Context, marker initializationRecord) bool {
	data, exists, err := area.Read(ctx, "ledgers/"+name, ledgerMaximum)
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
	key, exists, err := area.Read(ctx, "keys/"+candidate.KeyID+".bin", 32)
	if err != nil || !exists || len(key) != 32 {
		clear(key)
		return false
	}
	_, err = decodeLedger(data, selected.ID, marker.Selection, key, candidate.KeyID, 0)
	clear(key)
	return err == nil
}

func initializationSigningKey(ctx context.Context, area storage.Area, marker initializationRecord) ([]byte, error) {
	key, exists, err := area.Read(ctx, "keys/"+marker.MACKeyID+".bin", 32)
	if err != nil || !exists || len(key) != 32 {
		clear(key)
		return nil, storage.Failure("store.corrupt", "secret initialization authentication key is missing or unsafe")
	}
	if !verifyInitializationMAC(marker, key) {
		clear(key)
		return nil, storage.Failure("store.corrupt", "secret initialization authentication is invalid")
	}
	return key, nil
}

func onlyPending(entries []storage.Entry) bool {
	for _, entry := range entries {
		if entry.Directory || !validPending(entry.Name) {
			return false
		}
	}
	return true
}

func optionalEntryNames(ctx context.Context, area storage.Area, directory string) (map[string]bool, error) {
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

func namedEntry(ctx context.Context, area storage.Area, directory, name string) (*storage.Entry, error) {
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
