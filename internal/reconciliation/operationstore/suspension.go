package operationstore

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"path"
	"slices"

	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

const (
	SuspensionVersion     = "sign-on-suspension-v1"
	MaxSuspensionBytes    = 16 << 10
	MaxPreparationBytes   = 12 << 10
	suspensionsDirectory  = "suspensions"
	SuspensionSuspending  = "suspending"
	SuspensionSuspended   = "suspended"
	SuspensionResuming    = "resuming"
	suspensionDigestBytes = 32
)

type Suspension struct {
	Block       string          `json:"block"`
	Preparation json.RawMessage `json:"preparation"`
	Request     string          `json:"request"`
	State       string          `json:"state"`
	Version     string          `json:"version"`
}

func (s *Store) suspensionPath(id, block string) string {
	return path.Join(id, suspensionsDirectory, block+".json")
}

func validateSuspension(record Suspension) error {
	digest, err := hex.DecodeString(record.Request)
	switch {
	case record.Version != SuspensionVersion:
		return recordError("sign-on suspension record names another version")
	case !reconciliation.ValidSegment(record.Block):
		return recordError("sign-on suspension record names an invalid block")
	case err != nil || len(digest) != suspensionDigestBytes || hex.EncodeToString(digest) != record.Request:
		return recordError("sign-on suspension record names an invalid request digest")
	case !slices.Contains([]string{SuspensionSuspending, SuspensionSuspended, SuspensionResuming}, record.State):
		return recordError("sign-on suspension record names an unknown state")
	case len(record.Preparation) == 0 || len(record.Preparation) > MaxPreparationBytes || canonicaljson.ProveObject(record.Preparation) != nil:
		return recordError("sign-on suspension record holds no bounded canonical preparation")
	}
	return nil
}

func (s *Store) ReadSuspension(ctx context.Context, id, block string) (Suspension, bool, error) {
	if !reconciliation.ValidOperationID(id) || !reconciliation.ValidSegment(block) {
		return Suspension{}, false, defectError("sign-on suspension identity is invalid")
	}
	target := s.suspensionPath(id, block)
	data, found, err := s.area.Read(ctx, target, MaxSuspensionBytes)
	if err != nil {
		return Suspension{}, false, err
	}
	if !found {
		s.remember(target, nil)
		return Suspension{}, false, nil
	}
	var record Suspension
	if err := decodeUnder(data, MaxSuspensionBytes, &record, s.running); err != nil {
		return Suspension{}, false, err
	}
	if err := validateSuspension(record); err != nil {
		return Suspension{}, false, err
	}
	if record.Block != block {
		return Suspension{}, false, recordError("sign-on suspension record contradicts its location")
	}
	s.remember(target, slices.Clone(data))
	return record, true, nil
}

func (s *Store) PublishSuspension(ctx context.Context, id string, record Suspension) error {
	if !reconciliation.ValidOperationID(id) {
		return defectError("sign-on suspension identity is invalid")
	}
	if err := validateSuspension(record); err != nil {
		return err
	}
	encoded, err := encode(record, MaxSuspensionBytes)
	if err != nil {
		return err
	}
	target := s.suspensionPath(id, record.Block)
	expected, read := s.expectation(target)
	if !read {
		return defectError("sign-on suspension replacement lacks its exact read expectation")
	}
	if err := s.area.EnsureDirectory(ctx, path.Join(id, suspensionsDirectory)); err != nil {
		return err
	}
	if err := s.area.Replace(ctx, target, encoded, expected); err != nil {
		return err
	}
	s.remember(target, slices.Clone(encoded))
	return nil
}

func (s *Store) ClearSuspension(ctx context.Context, id, block string) error {
	if !reconciliation.ValidOperationID(id) || !reconciliation.ValidSegment(block) {
		return defectError("sign-on suspension identity is invalid")
	}
	target := s.suspensionPath(id, block)
	expected, read := s.expectation(target)
	if !read {
		return defectError("sign-on suspension removal lacks its exact read expectation")
	}
	if expected == nil {
		return nil
	}
	if err := s.area.RemoveRecord(ctx, target, expected); err != nil {
		return err
	}
	s.remember(target, nil)
	return nil
}
