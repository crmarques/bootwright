package v1alpha1

import (
	"slices"
	"strconv"
	"strings"
)

type Unrealized struct {
	Kind  Kind
	Path  string
	Value string
	Item  string
}

var unrealized = []Unrealized{
	{InfraProvider, "$.spec.vsphere", "", "B99"},
	{MachineInstallProfile, "$.spec.installer.templateClone", "", "B99"},
	{Registry, "$.spec.management", "managed", "B68"},
	{LoadBalancer, "$.spec.management", "managed", "B100"},
	{OpenShiftCluster, "$.spec.distribution.type", "okd", "B748"},
	{OpenShiftCluster, "$.spec.install.mode", "disconnected", "B69"},
	{OpenShiftCluster, "$.spec.install.servingCertificates", "", "B723"},
	{ClusterAddonBinding, "$.spec.addonRefs", "", "B90"},
	{ClusterAddonBinding, "$.spec.profileRefs", "", "B90"},
	{CustomPlaybook, "$.spec.enabled", "true", "B102"},
}

func UnrealizedDeclarations() []Unrealized { return slices.Clone(unrealized) }

func (u Unrealized) DeclaredBy(object Object) bool {
	if object.Kind() != u.Kind {
		return false
	}
	value := object.Spec().Get(strings.Split(strings.TrimPrefix(u.Path, "$.spec."), ".")...)
	switch {
	case !value.Present():
		return false
	case value.Type() == Boolean:
		return strconv.FormatBool(value.Bool()) == u.Value
	case u.Value != "":
		return value.Text() == u.Value
	case value.Type() == Sequence:
		return value.Len() > 0
	}
	return true
}

func (u Unrealized) Declaration() string {
	path := strings.TrimPrefix(u.Path, "$.")
	if u.Value != "" {
		return path + ": " + u.Value
	}
	return path
}
