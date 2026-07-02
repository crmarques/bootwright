package v1alpha1

import (
	"bytes"
	"encoding/json"

	"go.yaml.in/yaml/v3"
)

// StorageFilesystem owns one CephFS filesystem and its MDS placement.
// Deleting the object from desired state leaves the live filesystem
// running (the storage-wide additive-only rule on StorageCluster).
type StorageFilesystem struct {
	APIVersion string                `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                `yaml:"kind" json:"kind"`
	Metadata   Metadata              `yaml:"metadata" json:"metadata"`
	Spec       StorageFilesystemSpec `yaml:"spec" json:"spec"`
	SourcePath string                `yaml:"-" json:"-"`
}

type StorageFilesystemSpec struct {
	StorageClusterRef LocalObjectReference `yaml:"storageClusterRef" json:"storageClusterRef"`
	CephFS            StorageCephFSSpec    `yaml:"cephfs" json:"cephfs"`
}

type StorageCephFSSpec struct {
	MetadataPoolRef LocalObjectReference          `yaml:"metadataPoolRef" json:"metadataPoolRef"`
	DataPoolRefs    []StorageCephFSDataPoolRef    `yaml:"dataPoolRefs" json:"dataPoolRefs"`
	MDS             StorageCephFSMetadataServices `yaml:"mds,omitempty" json:"mds,omitempty"`
	// SubvolumeGroups declares static subvolume groups (`ceph fs subvolumegroup
	// create`) — the multi-tenant boundary tools like CSI provision subvolumes
	// into. Individual subvolumes are deliberately out of scope (apps/CSI own
	// those). Additive-only: a removed group keeps running.
	SubvolumeGroups []StorageCephFSSubvolumeGroup `yaml:"subvolumeGroups,omitempty" json:"subvolumeGroups,omitempty"`
}

// StorageCephFSSubvolumeGroup mirrors `ceph fs subvolumegroup create` flags.
type StorageCephFSSubvolumeGroup struct {
	Name string `yaml:"name" json:"name"`
	// PoolLayoutRef names the StoragePool the group's data lands in
	// (--pool_layout); same cluster as the filesystem.
	PoolLayoutRef LocalObjectReference `yaml:"poolLayoutRef,omitempty" json:"poolLayoutRef,omitempty"`
	// Mode is the group directory mode in octal (--mode, e.g. "0755").
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
	// UID / GID own the group directory (--uid / --gid); pointers so 0 (root) is
	// distinguishable from unset.
	UID *int `yaml:"uid,omitempty" json:"uid,omitempty"`
	GID *int `yaml:"gid,omitempty" json:"gid,omitempty"`
	// SizeBytes is the group quota in bytes (--size).
	SizeBytes int64 `yaml:"sizeBytes,omitempty" json:"sizeBytes,omitempty"`
}

// StorageCephFSDataPoolRef names one data pool backing the filesystem. It is
// authored as a plain pool name; the {name, default} object form exists only
// to elect the default data pool on multi-pool filesystems (a single entry
// defaults automatically).
type StorageCephFSDataPoolRef struct {
	Name    string `yaml:"name" json:"name"`
	Default bool   `yaml:"default,omitempty" json:"default,omitempty"`
}

// storageCephFSDataPoolRef mirrors StorageCephFSDataPoolRef without its
// methods so the codecs below can decode the object form without recursing.
type storageCephFSDataPoolRef StorageCephFSDataPoolRef

func (r *StorageCephFSDataPoolRef) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		name, err := decodeRefScalar(node)
		if err != nil {
			return err
		}
		*r = StorageCephFSDataPoolRef{Name: name}
		return nil
	}
	var out storageCephFSDataPoolRef
	if err := decodeKnownYAMLNode(node, &out); err != nil {
		return err
	}
	*r = StorageCephFSDataPoolRef(out)
	return nil
}

func (r *StorageCephFSDataPoolRef) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err == nil {
		*r = StorageCephFSDataPoolRef{Name: name}
		return nil
	}
	var out storageCephFSDataPoolRef
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		return err
	}
	*r = StorageCephFSDataPoolRef(out)
	return nil
}

type StorageCephFSMetadataServices struct {
	ActiveCount int `yaml:"activeCount,omitempty" json:"activeCount,omitempty"`
	// StandbyReplay enables hot standby-replay MDS daemons
	// (`ceph fs set <fs> allow_standby_replay true`), the standard production
	// HA posture. StandbyCountWanted is the number of standby daemons the
	// cluster wants (`standby_count_wanted`); both spell the native fields.
	StandbyReplay      bool                       `yaml:"standbyReplay,omitempty" json:"standbyReplay,omitempty"`
	StandbyCountWanted int                        `yaml:"standbyCountWanted,omitempty" json:"standbyCountWanted,omitempty"`
	Placement          StoragePlacement           `yaml:"placement,omitempty" json:"placement,omitempty"`
	ServiceSpec        *StorageCephMDSServiceSpec `yaml:"serviceSpec,omitempty" json:"serviceSpec,omitempty"`
}

// StorageCephMDSServiceSpec exposes the cephadm common service-spec fields for
// the MDS service (all top-level service-spec keys, not daemon config). Scoped
// to MDS on purpose; per-MDS config belongs in spec.ceph.config[mds.<fs>].
type StorageCephMDSServiceSpec struct {
	// Unmanaged freezes the MDS daemon set (cephadm stops reconciling it).
	Unmanaged bool `yaml:"unmanaged,omitempty" json:"unmanaged,omitempty"`
	// ExtraContainerArgs / ExtraEntrypointArgs pass through to the daemon
	// container (extra_container_args / extra_entrypoint_args).
	ExtraContainerArgs  []string `yaml:"extraContainerArgs,omitempty" json:"extraContainerArgs,omitempty"`
	ExtraEntrypointArgs []string `yaml:"extraEntrypointArgs,omitempty" json:"extraEntrypointArgs,omitempty"`
	// Networks pins the daemon to one or more CIDRs (networks).
	Networks []string `yaml:"networks,omitempty" json:"networks,omitempty"`
}
