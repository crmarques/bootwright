package converge

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/internal/converge/remedy"
)

const HostSharedServiceManifestExtraVar = "bootwright_host_shared_service_manifest"

type HostSharedServiceConsequence struct {
	Kind             string   `json:"kind"`
	Name             string   `json:"name"`
	SelectionDigests []string `json:"selectionDigests"`
	ClaimDigests     []string `json:"claimDigests,omitempty"`
}

type HostSharedServiceSelection struct {
	APIVersion   string                         `json:"apiVersion"`
	Kind         string                         `json:"kind"`
	Context      string                         `json:"context"`
	Command      string                         `json:"command"`
	Host         string                         `json:"host"`
	Consequences []HostSharedServiceConsequence `json:"consequences"`
}

type HostSharedServiceManifest map[string]HostSharedServiceSelection

type HostSharedServiceManifestError struct {
	Err error
}

func (e *HostSharedServiceManifestError) Error() string {
	return "cannot prove the exact selected host shared-service consequence set: " + e.Err.Error()
}

func (e *HostSharedServiceManifestError) Unwrap() error {
	return e.Err
}

func (e *HostSharedServiceManifestError) Remedy() remedy.Request {
	return remedy.Request{Action: remedy.ActionRetrySameInvocation}
}

func hostSharedServiceManifestError(err error) error {
	return &HostSharedServiceManifestError{Err: err}
}

func BuildHostSharedServiceManifest(contextName, command string, refs []InfraComponentServiceRef) (HostSharedServiceManifest, error) {
	contextName = strings.TrimSpace(contextName)
	if contextName == "" {
		return nil, hostSharedServiceManifestError(errors.New("host shared-service manifest requires a context name"))
	}
	if command != "apply" && command != "destroy" {
		return nil, hostSharedServiceManifestError(fmt.Errorf("host shared-service manifest does not support command %q", command))
	}
	type identity struct {
		host string
		kind string
		name string
	}
	type digestSets struct {
		selection map[string]bool
		claim     map[string]bool
	}
	digestsByIdentity := map[identity]digestSets{}
	for _, ref := range refs {
		host := strings.TrimSpace(ref.Host)
		kind := strings.TrimSpace(ref.Kind)
		name := strings.TrimSpace(ref.Name)
		if host == "" || kind == "" || name == "" || host != ref.Host || kind != ref.Kind || name != ref.Name {
			return nil, hostSharedServiceManifestError(fmt.Errorf("host shared-service consequence has no exact kind/name/host identity: kind=%q name=%q host=%q", ref.Kind, ref.Name, ref.Host))
		}
		key := identity{host: host, kind: kind, name: name}
		if len(ref.SelectionDigests) == 0 {
			return nil, hostSharedServiceManifestError(fmt.Errorf("host shared-service consequence %s/%s on %s has no exact selected-input digest", kind, name, host))
		}
		sets := digestsByIdentity[key]
		if sets.selection == nil {
			sets.selection = map[string]bool{}
			sets.claim = map[string]bool{}
		}
		for _, digest := range ref.SelectionDigests {
			if !validSharedServiceDigest(digest) {
				return nil, hostSharedServiceManifestError(fmt.Errorf("host shared-service consequence %s/%s on %s has invalid selected-input digest %q", kind, name, host, digest))
			}
			sets.selection[digest] = true
		}
		for _, digest := range ref.ClaimDigests {
			if !validSharedServiceDigest(digest) {
				return nil, hostSharedServiceManifestError(fmt.Errorf("host shared-service consequence %s/%s on %s has invalid physical claim digest %q", kind, name, host, digest))
			}
			sets.claim[digest] = true
		}
		digestsByIdentity[key] = sets
	}
	byHost := map[string][]HostSharedServiceConsequence{}
	for key, sets := range digestsByIdentity {
		selectionDigests := make([]string, 0, len(sets.selection))
		for digest := range sets.selection {
			selectionDigests = append(selectionDigests, digest)
		}
		var claimDigests []string
		if len(sets.claim) > 0 {
			claimDigests = make([]string, 0, len(sets.claim))
		}
		for digest := range sets.claim {
			claimDigests = append(claimDigests, digest)
		}
		sort.Strings(selectionDigests)
		sort.Strings(claimDigests)
		byHost[key.host] = append(byHost[key.host], HostSharedServiceConsequence{
			Kind:             key.kind,
			Name:             key.name,
			SelectionDigests: selectionDigests,
			ClaimDigests:     claimDigests,
		})
	}
	manifest := HostSharedServiceManifest{}
	for host, consequences := range byHost {
		sort.Slice(consequences, func(i, j int) bool {
			if consequences[i].Kind != consequences[j].Kind {
				return consequences[i].Kind < consequences[j].Kind
			}
			return consequences[i].Name < consequences[j].Name
		})
		manifest[host] = HostSharedServiceSelection{
			APIVersion:   "bootwright.io/host-shared-service-selection/v1alpha1",
			Kind:         "host-shared-service-selection",
			Context:      contextName,
			Command:      command,
			Host:         host,
			Consequences: consequences,
		}
	}
	return manifest, nil
}

func validSharedServiceDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, r := range value[len("sha256:"):] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func (m HostSharedServiceManifest) Hosts() []string {
	hosts := make([]string, 0, len(m))
	for host := range m {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	return hosts
}

func (m HostSharedServiceManifest) ExtraVarPair() (string, error) {
	if len(m) == 0 {
		return "", nil
	}
	encoded, err := json.Marshal(map[string]HostSharedServiceManifest{
		HostSharedServiceManifestExtraVar: m,
	})
	if err != nil {
		return "", fmt.Errorf("encode host shared-service manifest: %w", err)
	}
	return string(encoded), nil
}
