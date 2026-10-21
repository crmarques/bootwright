package clusterobjects

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func Canonical(value any) ([]byte, error) {
	data, err := canonicaljson.Encode(value, canonicaljson.Bare)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var generic any
	if err := decoder.Decode(&generic); err != nil {
		return nil, err
	}
	return reconciliation.Freeze(generic, "cluster object")
}

func valueJSON(v api.Value) (any, error) {
	switch v.Type() {
	case api.String:
		return v.Text(), nil
	case api.Boolean:
		return v.Bool(), nil
	case api.Integer, api.Number:
		return json.Number(v.Text()), nil
	case api.Mapping:
		result := map[string]any{}
		for _, f := range v.Fields() {
			if _, exists := result[f.Name]; exists {
				return nil, fmt.Errorf("duplicate object field %s", f.Name)
			}
			value, err := valueJSON(f.Value)
			if err != nil {
				return nil, err
			}
			result[f.Name] = value
		}
		return result, nil
	case api.Sequence:
		result := make([]any, 0, v.Len())
		for _, item := range v.Items() {
			value, err := valueJSON(item)
			if err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("absent object value")
	}
}

func NewObjectEntry(object Object) (ObjectEntry, error) {
	value, err := valueJSON(object.Body)
	if err != nil {
		return ObjectEntry{}, err
	}
	body, err := Canonical(value)
	if err != nil {
		return ObjectEntry{}, err
	}
	entry := ObjectEntry{Ref: object.Ref, Body: body, DesiredDigest: DesiredDigest(body)}
	return entry, validateObject(entry)
}

func DesiredDigest(body []byte) string { return hash(ObjectDomain, string(body)) }

func OwnerMark(operation, block string, ref Ref) string {
	group := ""
	if parts := strings.SplitN(ref.APIVersion, "/", 2); len(parts) == 2 {
		group = parts[0]
	}
	return hash(OwnerDomain, operation, block, group, ref.Kind, ref.Namespace, ref.Name)
}

func FieldManager(operation, block string) string {
	return "bootwright-" + hash(OwnerDomain, operation, block)[:32]
}

func hash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func NewPlacementEntry(placement SecretPlacement) (PlacementEntry, error) {
	body, err := Canonical(placement)
	if err != nil {
		return PlacementEntry{}, err
	}
	return PlacementEntry{SecretPlacement: placement, DesiredDigest: DesiredDigest(body)}, nil
}

func CompilePhase(phase Phase) ([]Step, error) {
	steps := []Step{}
	if len(phase.Gate) > 0 {
		steps = append(steps, Step{Kind: "wait", Wait: phase.Gate})
	}
	for _, secret := range phase.Secrets {
		entry, err := NewPlacementEntry(secret)
		if err != nil {
			return nil, err
		}
		steps = append(steps, Step{Kind: "place", Place: &entry})
	}
	for _, object := range phase.Objects {
		entry, err := NewObjectEntry(object)
		if err != nil {
			return nil, err
		}
		steps = append(steps, Step{Kind: "create", Create: &entry})
	}
	for _, approval := range phase.Approvals {
		steps = append(steps, Step{Kind: "approveDependent", ApproveDependent: &approval})
	}
	return steps, nil
}

func Freeze(program Program, options Options) ([]byte, error) {
	if err := Validate(program, options); err != nil {
		return nil, err
	}
	data, err := Canonical(program)
	if err != nil {
		return nil, err
	}
	if len(data) > reconciliation.MaxRequestBytes {
		return nil, fmt.Errorf("cluster object program exceeds request bound")
	}
	return data, nil
}

func ThawVersion(data []byte, options Options) (Program, error) {
	if len(data) > reconciliation.MaxRequestBytes {
		return Program{}, fmt.Errorf("cluster object program exceeds request bound")
	}
	if _, err := reconciliation.ThawVersion[map[string]json.RawMessage](data, "cluster object program", Version); err != nil {
		return Program{}, err
	}
	program, err := reconciliation.Thaw[Program](data, "cluster object program")
	if err != nil {
		return Program{}, err
	}
	encoded, err := Canonical(program)
	if err != nil || !bytes.Equal(encoded, data) {
		return Program{}, fmt.Errorf("cluster object program is not canonical")
	}
	if err := Validate(program, options); err != nil {
		return Program{}, err
	}
	return program, nil
}
