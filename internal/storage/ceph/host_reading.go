package ceph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const HostReadingVersion = "ceph-host-reading-v1"

const (
	hostsReading   = "hosts"
	maxHostReading = 64 << 10
)

var hostReadingActions = []string{"pool", "client", "policy", "rbd", "filesystem", "subvolume", "settings"}

type hostReadingEntry struct {
	Name    string          `json:"name"`
	State   json.RawMessage `json:"state"`
	Refused json.RawMessage `json:"refused"`
}

type hostReading struct {
	Version string             `json:"version"`
	Request string             `json:"request"`
	Scope   string             `json:"scope"`
	Hosts   []hostReadingEntry `json:"hosts"`

	Specializations []string `json:"specializations"`
}

var hostReadingResourceMembers = []string{"client", "filesystem", "gateway", "nfsExport", "nvmeof", "objectBucket", "objectUser", "policy", "pool", "rbd", "service", "settings", "subvolume"}

func hostReadingKey(r Request) (string, error) {
	scope := reflect.ValueOf(&r).Elem()
	for i := range scope.NumField() {
		name, _, _ := strings.Cut(scope.Type().Field(i).Tag.Get("json"), ",")
		if name == "action" || slices.Contains(hostReadingResourceMembers, name) {
			scope.Field(i).SetZero()
		}
	}
	data, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return HostReadingVersion + "\x00" + r.Cluster + "\x00" + hex.EncodeToString(digest[:]), nil
}

func (c Capability) sharedHosts(ctx context.Context, e lifecycle.Execution, r Request, removal bool) map[string]string {
	if e.Shared == nil || removal || lifecycle.Recorded(e) || !slices.Contains(hostReadingActions, r.Action) {
		return nil
	}
	key, err := hostReadingKey(r)
	if err != nil {
		return nil
	}
	reading, err := e.Shared.Read(ctx, key, func(inner context.Context) ([]byte, error) {
		result, err := c.invoke(inner, e, "observe", r, map[string]string{"reading": hostsReading}, nil)
		if err != nil {
			return nil, err
		}
		return decodeHostReading(result.Evidence, r, e.Block.RequestDigest)
	})
	if err != nil {
		return nil
	}
	return map[string]string{hostsReading: string(reading)}
}

func decodeHostReading(data []byte, r Request, digest string) ([]byte, error) {
	reading, err := reconciliation.DecodeEvidence[hostReading](data, maxHostReading, "Ceph host reading")
	if err != nil {
		return nil, evidenceFailure()
	}
	members, err := reconciliation.DecodeEvidence[map[string]json.RawMessage](data, maxHostReading, "Ceph host reading members")
	if err != nil || canonicaljson.ProveObject(data) != nil || !closedMembers(members, "hosts", "request", "scope", "version") && !closedMembers(members, "hosts", "request", "scope", "specializations", "version") {
		return nil, evidenceFailure()
	}
	if reading.Version != HostReadingVersion || reading.Request != digest || len(reading.Scope) != 64 || len(reading.Hosts) == 0 || len(reading.Hosts) > len(r.Hosts) {
		return nil, evidenceFailure()
	}
	for i, entry := range reading.Hosts {
		refused := len(entry.Refused) != 0
		if entry.Name != r.Hosts[i].Name || refused == (len(entry.State) != 0) || refused && i != len(reading.Hosts)-1 {
			return nil, evidenceFailure()
		}
	}
	if last := reading.Hosts[len(reading.Hosts)-1]; len(reading.Hosts) != len(r.Hosts) && len(last.Refused) == 0 {
		return nil, evidenceFailure()
	}
	return data, nil
}
