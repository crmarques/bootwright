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
func Decode(data []byte) (Store, error) {
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
	if err := store.Validate(); err != nil {
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
func (s Store) Validate() error {
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
		if seen && existing.HostKey() != record.HostKey() {
			return divergentPin(token, existing.Machine, record.Machine)
		}
		pinned[token] = record
	}
	return nil
}

// DivergentPin is the refusal of a store that would pin one endpoint to two
// keys. The store knows no context, so it names both Machines and the
// endpoint, and a caller that knows the context names the re-trust that
// settles it.
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

// Retrust restates the refusal for one context, naming the re-trust of the
// Machine whose record a refused write did not make: the record that diverges
// from what its endpoint presents now. writing reports the Machines the
// refused write records, and is nil for a store that was read. exempt reports
// why a Machine no longer uses this store, so a re-trust would refuse; its
// record is then dropped the one way the store allows, by the Machine leaving
// the input for one write, and the remedy states what that input change needs
// and what it costs a completed apply. The controller Machine leaves the input
// only with the controller reference, which the controller binding refuses
// once an apply has bound the context, so its remedy says so.
func (e *DivergentPin) Retrust(contextName string, writing func(string) bool, exempt func(string) string) error {
	message, other := e.message(), e.Machines[0]
	for index, name := range e.Machines {
		if writing != nil && writing(name) {
			other = e.Machines[1-index]
			message = "trusting " + name + " at " + e.Endpoint +
				" would pin it to a key that diverges from the one this context trusts there for " + other
			break
		}
	}
	reason := exemption(exempt, other)
	if reason == "" {
		return failure(message, "re-trust "+other+" with bootwright machine trust --context "+contextName+
			" --machines "+other+" --replace "+other)
	}
	message += "; " + other + " no longer uses this context's SSH trust (" + reason + "), so nothing reads that record, " +
		"but the store drops it only once " + other + " leaves the input"
	update := "bootwright context update --name " + contextName + " --input-dir <dir>, repeat this command, then restore " + other + " the same way"
	if reason == ExemptReachedLocally {
		return failure(message, other+" is the controller Machine, which leaves the input only when spec.controller.machineRef names another local Machine; "+
			"before an apply binds this context to "+other+", and with no incomplete operation, make that change with "+update+
			"; once an apply has bound this context, context update refuses any input that changes the controller Machine, "+
			"so no input edit drops the record and only a separate context does")
	}
	return failure(message, "drop "+other+" from the input with "+update+"; "+
		"this needs a context with no incomplete operation and an input in which no other object references "+other+
		", and after a completed apply the next apply no longer settles: it refuses the changed input until a destroy")
}

func exemption(exempt func(string) string, machine string) string {
	if exempt == nil {
		return ""
	}
	return exempt(machine)
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

// Supersede records one Machine's key and removes the record of every Machine
// the context no longer declares that holds the same endpoint, whatever its
// key: an address the context reassigned belongs to the Machine that declares
// it now. It returns what it removed, in Machine order. A record of a Machine
// declared still is kept, so a key that diverges from it stays a refusal of
// Validate rather than a silent replacement.
func (s *Store) Supersede(record Record, declared func(string) bool) []Record {
	endpoint := HostToken(record.Address, record.Port)
	kept := make([]Record, 0, len(s.Hosts)+1)
	var removed []Record
	for _, existing := range s.Hosts {
		if existing.Machine != record.Machine && declared != nil && !declared(existing.Machine) &&
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
