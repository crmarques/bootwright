package clusterobjects

import (
	"bytes"
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

func NewObjectEntry(object Object) (ObjectEntry, error) {
	value, err := object.Body.NativeChecked(api.NativeOptions{RefuseAbsent: true, RefuseDuplicates: true})
	if err != nil {
		return ObjectEntry{}, err
	}
	body, err := Canonical(value)
	if err != nil {
		return ObjectEntry{}, err
	}
	entry := ObjectEntry{Ref: object.Ref, Body: body, DesiredDigest: DesiredDigest(body)}
	entry.Exact, err = exactPaths(entry)
	if err != nil {
		return ObjectEntry{}, err
	}
	return entry, validateObject(entry)
}

var exactMembers = map[string][]string{
	"Subscription":       {"spec"},
	"Role":               {"rules", "subjects"},
	"ClusterRole":        {"rules", "subjects"},
	"RoleBinding":        {"rules", "subjects"},
	"ClusterRoleBinding": {"rules", "subjects"},
}

func exactPaths(entry ObjectEntry) ([]string, error) {
	var body map[string]json.RawMessage
	if err := canonicaljson.DecodeClosed(entry.Body, &body); err != nil {
		return nil, fmt.Errorf("invalid object body")
	}
	var paths []string
	for _, member := range exactMembers[entry.Kind] {
		if _, ok := body[member]; ok {
			paths = append(paths, member)
		}
	}
	return paths, nil
}

func DesiredDigest(body []byte) string {
	return reconciliation.ContentDigestOf(ObjectDomain, string(body))
}

func OwnerMark(operation, block string, ref Ref) string {
	group := ""
	if parts := strings.SplitN(ref.APIVersion, "/", 2); len(parts) == 2 {
		group = parts[0]
	}
	return reconciliation.ContentDigestOf(OwnerDomain, operation, block, group, ref.Kind, ref.Namespace, ref.Name)
}

func FieldManager(operation, block string) string {
	return "bootwright-" + reconciliation.ContentDigestOf(OwnerDomain, operation, block)[:32]
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
