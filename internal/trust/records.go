package trust

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
)

const (
	FormatVersion = 1

	// SourceEnrollment and SourceFirstUse name how a record entered the store,
	// so a later reader can tell a deliberate enrollment from a key an operator
	// confirmed once at a prompt.
	SourceEnrollment = "machine trust"
	SourceFirstUse   = "first use"

	// ExemptReachedLocally is the exemption of the controller Machine, the one
	// Machine reached locally, which a bound context's input never drops.
	ExemptReachedLocally = "reached locally"

	MaxHosts      = 4096
	maxStoreBytes = 256 << 10
)

// Record is one Machine's trusted server identity. It holds public material
// only: nothing here is confidential, and nothing here is desired state.
type Record struct {
	Machine     string `json:"machine"`
	Address     string `json:"address"`
	Port        int    `json:"port"`
	KeyType     string `json:"keyType"`
	PublicKey   string `json:"publicKey"`
	Fingerprint string `json:"fingerprint"`
	Source      string `json:"source"`
	Recorded    string `json:"recorded"`
}

func (r Record) HostKey() HostKey { return HostKey{Type: r.KeyType, PublicKey: r.PublicKey} }

type Store struct {
	FormatVersion int      `json:"formatVersion"`
	Hosts         []Record `json:"hosts"`
}

// Decode reads the context's trust records. Absent material is an empty store
// rather than a failure, because a context that has trusted nothing yet is
// ordinary rather than damaged.
func Decode(data []byte) (Store, error) { return decode(data, true) }

// DecodeForRepair reads the records as Decode does, except that a store that
// pins one endpoint to divergent keys is returned for machine trust to repair.
func DecodeForRepair(data []byte) (Store, error) { return decode(data, false) }

func decode(data []byte, pins bool) (Store, error) {
	store := Store{FormatVersion: FormatVersion, Hosts: []Record{}}
	if len(bytes.TrimSpace(data)) == 0 {
		return store, nil
	}
	if len(data) > maxStoreBytes {
		return Store{}, failure("the SSH trust store exceeds its byte limit", "")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&store); err != nil {
		return Store{}, failure("the SSH trust store could not be decoded", "")
	}
	if len(bytes.Trim(data[decoder.InputOffset():], " \t\r\n")) != 0 {
		return Store{}, failure("the SSH trust store carries trailing content", "")
	}
	if store.FormatVersion != FormatVersion {
		return Store{}, failure("the SSH trust store was written in another format version", "")
	}
	if store.Hosts == nil {
		store.Hosts = []Record{}
	}
	if err := store.validate(pins); err != nil {
		return Store{}, err
	}
	store.sort()
	return store, nil
}

func (s Store) Encode() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	s.Hosts = slices.Clone(s.Hosts)
	s.sort()
	s.FormatVersion = FormatVersion
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, failure("the SSH trust store could not be encoded", "")
	}
	data = append(data, '\n')
	if len(data) > maxStoreBytes {
		return nil, failure("the SSH trust store exceeds its byte limit", "")
	}
	return data, nil
}

// Validate refuses a store no session could read unambiguously. OpenSSH
// accepts any known_hosts entry matching a host, so two records pinning one
// address to different keys would weaken the pin from "exactly this key" to
// "either key"; that is enforced here, at the one point that writes.
func (s Store) Validate() error { return s.validate(true) }

func (s Store) validate(pins bool) error {
	if len(s.Hosts) > MaxHosts {
		return failure("the SSH trust store exceeds its record limit", "")
	}
	machines := map[string]bool{}
	pinned := map[string]Record{}
	for _, record := range s.Hosts {
		switch {
		case record.Machine == "" || record.Address == "":
			return failure("an SSH trust record names no Machine and address", "")
		case !known(record.KeyType) || record.PublicKey == "":
			return failure("an SSH trust record carries no qualified host key", "")
		case machines[record.Machine]:
			return failure("the SSH trust store records Machine "+record.Machine+" more than once", "")
		}
		machines[record.Machine] = true
		token := HostToken(record.Address, record.Port)
		existing, seen := pinned[token]
		if pins && seen && existing.HostKey() != record.HostKey() {
			return divergentPin(token, existing.Machine, record.Machine)
		}
		pinned[token] = record
	}
	return nil
}

// DivergentPin is the refusal of a store that pins, or would pin, one endpoint
// to two keys. Every reader refuses such a store; machine trust repairs it.
type DivergentPin struct {
	Endpoint string
	Machines [2]string
	refusal  error
}

func divergentPin(endpoint, first, second string) *DivergentPin {
	pin := &DivergentPin{Endpoint: endpoint, Machines: [2]string{first, second}}
	pin.refusal = failure(pin.message(), "")
	return pin
}

func (e *DivergentPin) Error() string { return e.refusal.Error() }

func (e *DivergentPin) Unwrap() error { return e.refusal }

func (e *DivergentPin) message() string {
	return "the SSH trust store pins " + e.Endpoint + " to divergent keys for " + e.Machines[0] + " and " + e.Machines[1]
}

// Retrust restates the refusal for one context, naming the machine trust that
// settles it. writing reports the Machines a refused write records, nil for a
// stored file; uses reports the Machines that read this store, nil for all.
func (e *DivergentPin) Retrust(contextName string, writing, uses func(string) bool) error {
	message, written, other := e.message(), "", ""
	for index, name := range e.Machines {
		if writing != nil && writing(name) {
			written, other = name, e.Machines[1-index]
			message = "trusting " + name + " at " + e.Endpoint +
				" would pin it to a key that diverges from the one this context trusts there for " + other
			break
		}
	}
	command := "bootwright machine trust --context " + contextName
	if written != "" && (uses == nil || uses(other)) {
		return failure(message, "verify the new fingerprint out of band, then re-trust "+other+" with "+command+
			" --name "+other+" --replace "+other)
	}
	var used, unused []string
	for _, name := range e.Machines {
		if uses == nil || uses(name) {
			used = append(used, name)
		} else {
			unused = append(unused, name)
		}
	}
	slices.Sort(used)
	slices.Sort(unused)
	switch len(unused) {
	case 0:
		return failure(message, "re-trust "+used[0]+" and "+used[1]+" with "+command+" --name "+strings.Join(used, ","))
	case 1:
		return failure(message, "re-trust "+used[0]+" with "+command+" --name "+used[0]+
			"; its confirmed write removes the record of "+unused[0]+", which no longer uses this context's SSH trust")
	default:
		return failure(message, "repair it with "+command+"; its confirmed write removes the records of "+
			unused[0]+" and "+unused[1]+", which no longer use this context's SSH trust")
	}
}

// DropDivergentUnused removes, at each endpoint the store pins to more than one
// key, the record of every Machine uses reports false for, and returns what it
// removed in Machine order. A nil uses removes nothing.
func (s *Store) DropDivergentUnused(uses func(machine string) bool) []Record {
	if uses == nil {
		return nil
	}
	keys := map[string]HostKey{}
	divergent := map[string]bool{}
	for _, record := range s.Hosts {
		token := HostToken(record.Address, record.Port)
		if key, seen := keys[token]; seen && key != record.HostKey() {
			divergent[token] = true
		}
		keys[token] = record.HostKey()
	}
	kept := make([]Record, 0, len(s.Hosts))
	var removed []Record
	for _, record := range s.Hosts {
		if divergent[HostToken(record.Address, record.Port)] && !uses(record.Machine) {
			removed = append(removed, record)
			continue
		}
		kept = append(kept, record)
	}
	s.Hosts = kept
	slices.SortFunc(removed, byMachine)
	return removed
}

func (s Store) Find(machine string) (Record, bool) {
	for _, record := range s.Hosts {
		if record.Machine == machine {
			return record, true
		}
	}
	return Record{}, false
}

func (s *Store) Upsert(record Record) {
	for i := range s.Hosts {
		if s.Hosts[i].Machine == record.Machine {
			s.Hosts[i] = record
			return
		}
	}
	s.Hosts = append(s.Hosts, record)
}

// Clone copies the records, so a candidate write never changes the store it
// was read from.
func (s Store) Clone() Store {
	return Store{FormatVersion: s.FormatVersion, Hosts: slices.Clone(s.Hosts)}
}

// Supersede records one Machine's key and removes the record of every other
// Machine at the same endpoint that does not use this store, whatever its key:
// an address the context reassigned, or holds for a Machine that no longer
// uses this store, belongs to the Machine that declares it now. It returns
// what it removed, in Machine order. A record of a Machine that still uses the
// store is kept, so a key that diverges from it stays a refusal of Validate
// rather than a silent replacement; a nil uses keeps every record.
func (s *Store) Supersede(record Record, uses func(machine string) bool) []Record {
	endpoint := HostToken(record.Address, record.Port)
	kept := make([]Record, 0, len(s.Hosts)+1)
	var removed []Record
	for _, existing := range s.Hosts {
		if existing.Machine != record.Machine && uses != nil && !uses(existing.Machine) &&
			HostToken(existing.Address, existing.Port) == endpoint {
			removed = append(removed, existing)
			continue
		}
		kept = append(kept, existing)
	}
	s.Hosts = kept
	s.Upsert(record)
	slices.SortFunc(removed, byMachine)
	return removed
}

func (s Store) sort() {
	slices.SortFunc(s.Hosts, byMachine)
}

func byMachine(a, b Record) int { return strings.Compare(a.Machine, b.Machine) }
