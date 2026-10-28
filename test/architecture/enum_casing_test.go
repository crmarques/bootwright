package architecture_test

import (
	"fmt"
	"regexp"
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

var lowerCamelValue = regexp.MustCompile(`^[a-z][a-zA-Z0-9]*$`)

// nativeEnumFields are the enum fields whose values a native product, protocol
// or the kind catalog spells, so specs/api.md's naming rule exempts them.
// Every other enum value is lowerCamel.
var nativeEnumFields = map[string]string{
	"ArtifactServer.spec.tls.minVersion":                                              "TLS protocol versions",
	"CephPool.spec.compression.algorithm":                                             "Ceph compression algorithms",
	"CephPool.spec.erasure.plugin":                                                    "Ceph erasure-code plugins",
	"CephRBDImage.spec.features[]":                                                    "RBD image features",
	"ClusterAddon.spec.inputs[].resourceKind":                                         "registered kind names",
	"ClusterAddon.spec.olm.subscription.installPlanApproval":                          "OLM install plan approvals",
	"ClusterAddonBinding.spec.packages.acm.hub.availabilityConfig":                    "MultiClusterHub availability",
	"ClusterAddonBinding.spec.packages.acm.hub.tolerations[].effect":                  "Kubernetes taint effects",
	"ClusterAddonBinding.spec.packages.acm.hub.tolerations[].operator":                "Kubernetes toleration operators",
	"Environment.spec.lifecycle.rescue.os.architecture":                               "machine architectures",
	"InfraProvider.spec.libvirt.uri":                                                  "libvirt connection URIs",
	"Machine.spec.substrate.baremetal.firmware.bootMode":                              "Redfish boot modes",
	"MachineInstallProfile.spec.os.architecture":                                      "machine architectures",
	"OpenShiftCluster.spec.install.additionalTrustBundlePolicy":                       "install-config trust bundle policies",
	"OpenShiftCluster.spec.install.registries.imageDigestSources[].sourcePolicy":      "image digest source policies",
	"OpenShiftCluster.spec.networking.ovnKubernetesConfig.gatewayConfig.ipForwarding": "OVN-Kubernetes IP forwarding modes",
	"OpenShiftCluster.spec.networking.secondaryNetworks[].ipam.lifecycle":             "OVN-Kubernetes IPAM lifecycles",
	"OpenShiftCluster.spec.networking.secondaryNetworks[].ipam.mode":                  "OVN-Kubernetes IPAM modes",
	"OpenShiftCluster.spec.networking.secondaryNetworks[].topology":                   "OVN-Kubernetes topologies",
	"OpenShiftCluster.spec.nodes[].taints[].effect":                                   "Kubernetes taint effects",
	"Registry.spec.implementation":                                                    "the mirror-registry product name",
}

func enumCasingViolations(path string, shape *api.Shape, exempt map[string]string, used map[string]bool, depth int) []string {
	if shape == nil || depth > 32 {
		return nil
	}
	var found []string
	if len(shape.Enums) > 0 {
		if _, ok := exempt[path]; ok {
			used[path] = true
		} else {
			for _, value := range shape.Enums {
				if !lowerCamelValue.MatchString(value) {
					found = append(found, fmt.Sprintf("%s: enum value %q is not lowerCamel; rename it, or list the field as native in nativeEnumFields", path, value))
				}
			}
		}
	}
	for _, field := range shape.Fields {
		found = append(found, enumCasingViolations(path+"."+field.Name, field.Shape, exempt, used, depth+1)...)
	}
	found = append(found, enumCasingViolations(path+"[]", shape.Element, exempt, used, depth+1)...)
	for _, alternative := range shape.Alternatives {
		found = append(found, enumCasingViolations(path, alternative, exempt, used, depth+1)...)
	}
	return found
}

// Every Bootwright-owned enum value of every registered schema is lowerCamel
// (specs/api.md, Naming new names); only the listed native fields keep the
// spelling their product owns, and the list names no field that no longer
// exists or holds no enum.
func TestEveryBootwrightEnumValueIsLowerCamel(t *testing.T) {
	used := map[string]bool{}
	for _, kind := range api.Kinds() {
		for _, violation := range enumCasingViolations(string(kind)+".spec", api.Schema(kind), nativeEnumFields, used, 0) {
			t.Error(violation)
		}
	}
	var stale []string
	for path := range nativeEnumFields {
		if !used[path] {
			stale = append(stale, path)
		}
	}
	slices.Sort(stale)
	for _, path := range stale {
		t.Errorf("nativeEnumFields lists %s, which is no enum field of a registered schema: remove it", path)
	}
	violator := &api.Shape{Type: api.Mapping, Fields: []api.Field{{Name: "transport", Shape: &api.Shape{Type: api.String, Enums: []string{"native", "verified-tunnel"}}}}}
	if len(enumCasingViolations("violator.spec", violator, nativeEnumFields, map[string]bool{}, 0)) != 1 {
		t.Error("the casing rule admits a kebab-case Bootwright value")
	}
}
