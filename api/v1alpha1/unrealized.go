package v1alpha1

import (
	"slices"
	"strconv"
	"strings"
)

type Unrealized struct {
	Kind   Kind
	Path   string
	Value  string
	Item   string
	Except string
}

var unrealized = []Unrealized{
	{Kind: InfraProvider, Path: "$.spec.vsphere", Item: "B99"},
	{Kind: MachineInstallProfile, Path: "$.spec.installer.templateClone", Item: "B99"},
	{Kind: Registry, Path: "$.spec.management", Value: "managed", Item: "B68"},
	{Kind: LoadBalancer, Path: "$.spec.management", Value: "managed", Item: "B100"},
	{Kind: OpenShiftCluster, Path: "$.spec.distribution.type", Value: "okd", Item: "B748"},
	{Kind: OpenShiftCluster, Path: "$.spec.install.mode", Value: "disconnected", Item: "B69"},
	{Kind: OpenShiftCluster, Path: "$.spec.install.servingCertificates", Item: "B723"},
	{Kind: ClusterAddonBinding, Path: "$.spec.addonRefs", Item: "B90"},
	{Kind: ClusterAddonBinding, Path: "$.spec.profileRefs", Item: "B90"},
	{Kind: CustomPlaybook, Path: "$.spec.enabled", Value: "true", Item: "B102"},
	{Kind: Entitlement, Path: "$.spec.rhsm.connectToInsights", Value: "true"},
	{Kind: Environment, Path: "$.spec.dependencyVersions.libvirt", Except: "latest", Item: "B676"},
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
	case u.Except != "":
		return value.Text() != u.Except
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
	if u.Except != "" {
		return path + " other than " + u.Except
	}
	return path
}
