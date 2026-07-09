package v1alpha1

import (
	"bytes"
	"encoding/json"

	"go.yaml.in/yaml/v3"
)

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
	SubvolumeGroups []StorageCephFSSubvolumeGroup `yaml:"subvolumeGroups,omitempty" json:"subvolumeGroups,omitempty"`
}

type StorageCephFSSubvolumeGroup struct {
	Name          string               `yaml:"name" json:"name"`
	PoolLayoutRef LocalObjectReference `yaml:"poolLayoutRef,omitempty" json:"poolLayoutRef,omitempty"`
	Mode          string               `yaml:"mode,omitempty" json:"mode,omitempty"`
	UID           *int                 `yaml:"uid,omitempty" json:"uid,omitempty"`
	GID           *int                 `yaml:"gid,omitempty" json:"gid,omitempty"`
	SizeBytes     int64                `yaml:"sizeBytes,omitempty" json:"sizeBytes,omitempty"`
}

type StorageCephFSDataPoolRef struct {
	Name    string `yaml:"name" json:"name"`
	Default bool   `yaml:"default,omitempty" json:"default,omitempty"`
}

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
	ActiveCount        int                        `yaml:"activeCount,omitempty" json:"activeCount,omitempty"`
	StandbyReplay      bool                       `yaml:"standbyReplay,omitempty" json:"standbyReplay,omitempty"`
	StandbyCountWanted int                        `yaml:"standbyCountWanted,omitempty" json:"standbyCountWanted,omitempty"`
	Placement          StoragePlacement           `yaml:"placement,omitempty" json:"placement,omitempty"`
	ServiceSpec        *StorageCephMDSServiceSpec `yaml:"serviceSpec,omitempty" json:"serviceSpec,omitempty"`
}

type StorageCephMDSServiceSpec struct {
	Unmanaged           bool     `yaml:"unmanaged,omitempty" json:"unmanaged,omitempty"`
	ExtraContainerArgs  []string `yaml:"extraContainerArgs,omitempty" json:"extraContainerArgs,omitempty"`
	ExtraEntrypointArgs []string `yaml:"extraEntrypointArgs,omitempty" json:"extraEntrypointArgs,omitempty"`
	Networks            []string `yaml:"networks,omitempty" json:"networks,omitempty"`
}
