// Package localstore implements the version-1 local encrypted secret store.
package localstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/storage"
)

const (
	storeType = "local-keyring"
	storeID   = "local-v1"
	custodyID = "local-keyfile-v1"
)

type Options struct {
	Random io.Reader
}

type Implementation struct {
	random io.Reader
}

func New() *Implementation { return NewWithOptions(Options{}) }

func NewWithOptions(options Options) *Implementation {
	if options.Random == nil {
		options.Random = rand.Reader
	}
	return &Implementation{random: options.Random}
}

func (i *Implementation) Selection() storage.Selection {
	return storage.Selection{
		Type: storeType,
		Store: storage.ComponentRef{
			ID: storeID, InterfaceVersion: 1, StateVersion: 1, ConfigVersion: 1,
		},
		KeyCustody: storage.ComponentRef{
			ID: custodyID, InterfaceVersion: 1, StateVersion: 1, ConfigVersion: 1,
		},
	}
}

func (*Implementation) Requirements() []storage.SessionRequirement {
	return []storage.SessionRequirement{}
}

func (i *Implementation) Initialize(ctx context.Context, selected storage.Context, area storage.Area, material storage.SessionMaterial) (storage.StoreSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if i == nil || i.random == nil || area == nil || material != nil || !validContext(selected) {
		return nil, storage.Failure("store.implementation", "local secret store initialization is not configured safely")
	}
	if selected.Mode != "ready" && selected.Mode != "initializing" {
		return nil, storage.Failure("store.conflict", "secret initialization requires a ready context")
	}
	return i.initialize(ctx, selected, area)
}

func (i *Implementation) Open(ctx context.Context, selected storage.Context, area storage.Area, selector storage.Selector, material storage.SessionMaterial) (storage.StoreSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if i == nil || i.random == nil || area == nil || material != nil || !validContext(selected) || !validSelector(selector, selected, i.Selection()) {
		return nil, storage.Failure("store.implementation", "persisted local secret store selection is incompatible")
	}
	selectorData, exists, err := area.ReadMutable(ctx, selectorPath, selectorMaximum)
	if err != nil {
		return nil, areaFailure(ctx, "store.corrupt", "secret selector cannot be read safely", err)
	}
	var actual storage.Selector
	if !exists || decodeCanonical(selectorData, selectorMaximum, 64, &actual) != nil || actual != selector {
		return nil, storage.Failure("store.corrupt", "secret selector changed or is invalid")
	}
	indexData, exists, err := area.Read(ctx, "indexes/"+selector.Generation+".bin", indexMaximum)
	if err != nil || !exists {
		return nil, areaFailure(ctx, "store.corrupt", "selected encrypted secret index is missing or unsafe", err)
	}
	defer clear(indexData)
	var wrapped envelope
	if decodeCanonical(indexData, indexMaximum, 32, &wrapped) != nil || wrapped.FormatVersion != formatVersion || wrapped.Algorithm != algorithm || wrapped.Purpose != "index" || wrapped.BlobID != selector.Generation || !validID(wrapped.KeyID, "key-") {
		return nil, storage.Failure("store.corrupt", "selected encrypted secret index is malformed")
	}
	key, err := readKey(ctx, area, wrapped.KeyID)
	if err != nil {
		return nil, err
	}
	keepKey := false
	defer func() {
		if !keepKey {
			clear(key)
		}
	}()
	plaintext, err := openEnvelope(indexData, key, indexAAD(selected.ID, selector, wrapped.KeyID), "index", wrapped.KeyID, selector.Generation, indexMaximum)
	if err != nil {
		return nil, err
	}
	defer clear(plaintext)
	var index indexRecord
	if decodeCanonical(plaintext, indexMaximum, maxIndexItems, &index) != nil || validateIndex(index, selector) != nil || index.ActiveKey != wrapped.KeyID {
		return nil, storage.Failure("store.corrupt", "decrypted secret index is invalid or inconsistent")
	}
	s := &session{implementation: i, context: selected, area: area, selector: selector, selectorData: slices.Clone(selectorData), index: cloneIndex(index), keys: map[string][]byte{wrapped.KeyID: key}}
	if err := s.refreshMetadata(ctx); err != nil {
		s.Close()
		return nil, err
	}
	keepKey = true
	return s, nil
}

type session struct {
	implementation *Implementation
	context        storage.Context
	area           storage.Area
	selector       storage.Selector
	selectorData   []byte
	index          indexRecord
	keys           map[string][]byte
	retained       int
	cleanup        bool
	closed         bool
	mutated        bool
}

func (s *session) usable(ctx context.Context, mutation bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.closed || s.area == nil || s.implementation == nil {
		return storage.Failure("store.implementation", "secret store session is closed or unavailable")
	}
	if mutation && s.mutated {
		return storage.Failure("store.conflict", "secret store session has already published a mutation")
	}
	return nil
}

func (s *session) Inspect(ctx context.Context) (storage.Snapshot, error) {
	if err := s.usable(ctx, false); err != nil {
		return storage.Snapshot{}, err
	}
	if err := s.refreshMetadata(ctx); err != nil {
		return storage.Snapshot{}, err
	}
	result := storage.Snapshot{Versions: make([]storage.Version, 0, len(s.index.Versions)), Current: slices.Clone(s.index.Current), Bindings: cloneBindings(s.index.Bindings), ActiveKey: s.index.ActiveKey, Keys: slices.Clone(s.index.Keys), RetainedArtifacts: s.retained, CleanupRequired: s.cleanup}
	for _, version := range s.index.Versions {
		parts := make([]secrets.Part, len(version.Parts))
		for i, part := range version.Parts {
			parts[i] = part.Part
		}
		result.Versions = append(result.Versions, storage.Version{ID: version.ID, Declaration: cloneDeclaration(version.Declaration), Parts: parts})
	}
	return result, nil
}

func (s *session) Read(ctx context.Context, versionID string) (secrets.Material, error) {
	if err := s.usable(ctx, false); err != nil {
		return secrets.Material{}, err
	}
	version, exists := findVersion(s.index, versionID)
	if !exists {
		return secrets.Material{}, storage.Failure("input", "secret version does not exist")
	}
	return s.readVersion(ctx, version)
}

func (s *session) readVersion(ctx context.Context, version storedVersion) (secrets.Material, error) {
	parts := make(map[secrets.Part][]byte, len(version.Parts))
	failed := true
	defer func() {
		if failed {
			for _, value := range parts {
				clear(value)
			}
		}
	}()
	for _, part := range version.Parts {
		if err := ctx.Err(); err != nil {
			return secrets.Material{}, err
		}
		key, err := s.key(ctx, part.KeyID)
		if err != nil {
			return secrets.Material{}, err
		}
		data, exists, err := s.area.Read(ctx, "parts/"+part.BlobID+".bin", partMaximum)
		if err != nil || !exists {
			return secrets.Material{}, areaFailure(ctx, "store.corrupt", "encrypted secret part is missing or unsafe", err)
		}
		plaintext, err := openEnvelope(data, key, partAAD(s.context.ID, s.selector.Selection, version, part), "part", part.KeyID, part.BlobID, partMaximum)
		clear(data)
		if err != nil {
			return secrets.Material{}, err
		}
		if len(plaintext) != part.Size || len(plaintext) > secrets.MaxPartBytes {
			clear(plaintext)
			return secrets.Material{}, storage.Failure("store.corrupt", "decrypted secret part has an invalid size")
		}
		parts[part.Part] = plaintext
	}
	material := secrets.NewMaterial(parts)
	for _, value := range parts {
		clear(value)
	}
	failed = false
	return material, nil
}

func (s *session) key(ctx context.Context, id string) ([]byte, error) {
	if key, exists := s.keys[id]; exists {
		return key, nil
	}
	if !indexHasKey(s.index, id) {
		return nil, storage.Failure("store.corrupt", "secret part references an unknown encryption key")
	}
	key, err := readKey(ctx, s.area, id)
	if err != nil {
		return nil, err
	}
	s.keys[id] = key
	return key, nil
}

func readKey(ctx context.Context, area storage.Area, id string) ([]byte, error) {
	data, exists, err := area.Read(ctx, "keys/"+id+".bin", 32)
	if err != nil || !exists || len(data) != 32 {
		clear(data)
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, storage.Failure("store.key-unavailable", "secret encryption key is missing or unsafe")
	}
	return data, nil
}

func (s *session) refreshMetadata(ctx context.Context) error {
	if err := validateIndex(s.index, s.selector); err != nil {
		return storage.Failure("store.corrupt", "secret index state is inconsistent")
	}
	for index, key := range s.index.Keys {
		keyMaterial, err := s.key(ctx, key.ID)
		if err != nil {
			return err
		}
		data, exists, err := s.area.ReadMutable(ctx, "ledgers/"+key.ID+".json", ledgerMaximum)
		if err != nil || !exists {
			return areaFailure(ctx, "store.corrupt", "secret seal ledger is missing or unsafe", err)
		}
		ledger, ledgerErr := decodeLedger(data, s.context.ID, s.selector.Selection, keyMaterial, key.ID, key.Seals)
		if ledgerErr != nil {
			return storage.Failure("store.corrupt", "secret seal ledger is invalid or contradictory")
		}
		s.index.Keys[index].Seals = ledger.Seals
	}
	retained, cleanup, err := inspectArtifacts(ctx, s.area, s.selector, s.index)
	if err != nil {
		return err
	}
	s.retained, s.cleanup = retained, cleanup
	return nil
}

func (s *session) Close() error {
	if s == nil || s.closed {
		return nil
	}
	for id, key := range s.keys {
		clear(key)
		delete(s.keys, id)
	}
	clear(s.selectorData)
	s.selectorData = nil
	s.index = indexRecord{}
	s.area = nil
	s.closed = true
	return nil
}

func (i *Implementation) uniqueID(prefix string, exists func(string) bool) (string, error) {
	if i == nil || i.random == nil {
		return "", storage.Failure("store.crypto", "secret identifier randomness is unavailable")
	}
	for range 16 {
		value := make([]byte, 16)
		if _, err := io.ReadFull(i.random, value); err != nil {
			clear(value)
			return "", storage.Failure("store.crypto", "secret identifier randomness is unavailable")
		}
		id := prefix + hex.EncodeToString(value)
		clear(value)
		if !exists(id) {
			return id, nil
		}
	}
	return "", storage.Failure("store.limit", "secret identifier collision limit is exhausted")
}

func validContext(context storage.Context) bool {
	return validName(context.Name) && validID(context.ID, "ctx-") && (context.Revision == "" || validID(context.Revision, "rev-")) && (context.Mode == "ready" || context.Mode == "initializing")
}

func validSelector(selector storage.Selector, context storage.Context, selection storage.Selection) bool {
	return selector.SelectorVersion == formatVersion && selector.ContextID == context.ID && selector.Selection == selection && validID(selector.Generation, "gen-")
}

func areaFailure(ctx context.Context, code, message string, err error) error {
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	var failure *desiredstate.Failure
	if errors.As(err, &failure) && len(failure.Diagnostics) == 1 && strings.HasPrefix(failure.Diagnostics[0].Code, "secret.store.") {
		return err
	}
	return storage.Failure(code, message)
}

func publicationFailure(ctx context.Context, outcome storage.Outcome, err error) error {
	if outcome == storage.NotCommitted {
		if canceled := ctx.Err(); canceled != nil {
			return canceled
		}
		return areaFailure(ctx, "store.conflict", "secret publication was not committed", err)
	}
	if outcome == storage.Uncertain {
		return storage.Failure("store.conflict", "secret publication has uncertain durability; inspect before retrying")
	}
	if outcome == storage.Committed {
		return storage.Failure("store.conflict", "secret publication committed but did not finish successfully; inspect before retrying")
	}
	return storage.Failure("store.conflict", "secret publication outcome is unknown; inspect before retrying")
}

func entryNames(ctx context.Context, area storage.Area, directory string) (map[string]bool, error) {
	entries, err := area.Entries(ctx, directory)
	if err != nil {
		return nil, areaFailure(ctx, "store.corrupt", "secret artifacts cannot be enumerated safely", err)
	}
	result := make(map[string]bool, len(entries))
	for _, entry := range entries {
		if entry.Directory || result[entry.Name] {
			return nil, storage.Failure("store.corrupt", "secret artifact directory is inconsistent")
		}
		result[entry.Name] = true
	}
	return result, nil
}

func validateIndex(index indexRecord, selector storage.Selector) error {
	if index.FormatVersion != formatVersion || index.Algorithm != algorithm || index.Selector != selector || !validID(index.ActiveKey, "key-") || index.Keys == nil || index.Versions == nil || index.Current == nil || index.Bindings == nil || len(index.Keys) == 0 || len(index.Versions) > secrets.MaxVersions || len(index.Bindings) > maxBindings {
		return errors.New("invalid index header")
	}
	keys := make(map[string]storage.Key, len(index.Keys))
	previous := ""
	active := 0
	for _, key := range index.Keys {
		if !validID(key.ID, "key-") || key.ID <= previous || key.Seals > maxSeals || key.State != "active" && key.State != "retired" {
			return errors.New("invalid index key")
		}
		if key.State == "active" {
			active++
			if key.ID != index.ActiveKey {
				return errors.New("contradictory active key")
			}
		}
		keys[key.ID] = key
		previous = key.ID
	}
	if active != 1 {
		return errors.New("invalid active key count")
	}
	versions := make(map[string]storedVersion, len(index.Versions))
	blobs := make(map[string]bool)
	total := 0
	previous = ""
	for _, version := range index.Versions {
		if !validID(version.ID, "ver-") || version.ID <= previous || !validateDeclaration(version.Declaration) || version.Parts == nil || len(version.Parts) != len(version.Declaration.Parts()) {
			return errors.New("invalid secret version")
		}
		expectedParts := version.Declaration.Parts()
		versionSize := 0
		for i, part := range version.Parts {
			if part.Part != expectedParts[i] || !validID(part.BlobID, "blob-") || !validID(part.KeyID, "key-") || !validID(part.Generation, "gen-") || part.Size < 0 || part.Size > secrets.MaxPartBytes || blobs[part.BlobID] {
				return errors.New("invalid secret part")
			}
			if _, exists := keys[part.KeyID]; !exists {
				return errors.New("unknown part key")
			}
			blobs[part.BlobID] = true
			versionSize += part.Size
		}
		if versionSize > secrets.MaxVersionBytes || total > secrets.MaxMaterialBytes-versionSize {
			return errors.New("secret material bound exceeded")
		}
		total += versionSize
		versions[version.ID] = version
		previous = version.ID
	}
	referenced := make(map[string]bool, len(versions))
	currentNames := make(map[string]bool, len(index.Current))
	previous = ""
	for _, current := range index.Current {
		version, exists := versions[current.Version]
		if !exists || !validName(current.Name) || current.Name <= previous || currentNames[current.Name] || version.Declaration.Name != current.Name || version.Declaration.Source == "file" {
			return errors.New("invalid current mapping")
		}
		currentNames[current.Name], referenced[current.Version] = true, true
		previous = current.Name
	}
	previous = ""
	for _, binding := range index.Bindings {
		if !validID(binding.ID, "bind-") || binding.ID <= previous || binding.Versions == nil || len(binding.Versions) == 0 || len(binding.Versions) > secrets.MaxVersions {
			return errors.New("invalid secret binding")
		}
		priorVersion := ""
		for _, id := range binding.Versions {
			if _, exists := versions[id]; !exists || id <= priorVersion {
				return errors.New("invalid bound version")
			}
			referenced[id] = true
			priorVersion = id
		}
		previous = binding.ID
	}
	if len(referenced) != len(versions) {
		return errors.New("unreferenced logical version")
	}
	return nil
}

func cloneIndex(index indexRecord) indexRecord {
	result := index
	result.Keys = slices.Clone(index.Keys)
	result.Current = slices.Clone(index.Current)
	result.Bindings = cloneBindings(index.Bindings)
	result.Versions = make([]storedVersion, len(index.Versions))
	for i, version := range index.Versions {
		result.Versions[i] = version
		result.Versions[i].Declaration = cloneDeclaration(version.Declaration)
		result.Versions[i].Parts = slices.Clone(version.Parts)
	}
	return result
}

func cloneBindings(bindings []storage.Binding) []storage.Binding {
	result := make([]storage.Binding, len(bindings))
	for i, binding := range bindings {
		result[i] = storage.Binding{ID: binding.ID, Versions: slices.Clone(binding.Versions)}
	}
	return result
}

func findVersion(index indexRecord, id string) (storedVersion, bool) {
	for _, version := range index.Versions {
		if version.ID == id {
			return version, true
		}
	}
	return storedVersion{}, false
}

func indexHasKey(index indexRecord, id string) bool {
	_, exists := slices.BinarySearchFunc(index.Keys, id, func(key storage.Key, id string) int { return strings.Compare(key.ID, id) })
	return exists
}
