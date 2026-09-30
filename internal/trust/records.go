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
			return failure("the SSH trust store pins "+token+" to divergent keys for "+
				existing.Machine+" and "+record.Machine,
				"re-trust one of them with bootwright machine trust --replace")
		}
		pinned[token] = record
	}
	return nil
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

func (s Store) sort() {
	slices.SortFunc(s.Hosts, func(a, b Record) int { return strings.Compare(a.Machine, b.Machine) })
}
