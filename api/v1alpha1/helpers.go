package v1alpha1

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

func InstallMode(cluster ContainerCluster) string {
	if cluster.Spec.Install.Mode == "" {
		return InstallModeConnected
	}
	return cluster.Spec.Install.Mode
}

func DistributionType(cluster ContainerCluster) string {
	if cluster.Spec.Distribution.Type == "" {
		return DistributionOpenShift
	}
	return cluster.Spec.Distribution.Type
}

func ReleaseChannel(cluster ContainerCluster) string {
	release := cluster.Spec.Distribution.Release
	if release.Channel != "" {
		return release.Channel
	}
	if DistributionType(cluster) != DistributionOpenShift || release.Image != "" {
		return ""
	}
	re := regexp.MustCompile(`^([0-9]+)[.]([0-9]+)[.]`)
	match := re.FindStringSubmatch(release.Version)
	if len(match) != 3 {
		return ""
	}
	return fmt.Sprintf("stable-%s.%s", match[1], match[2])
}

func ReleaseImageSource(cluster ContainerCluster) string {
	return ImageReferenceSource(cluster.Spec.Distribution.Release.Image)
}

func ImageReferenceSource(image string) string {
	ref := strings.TrimSpace(image)
	if ref == "" {
		return ""
	}
	if at := strings.Index(ref, "@"); at >= 0 {
		ref = ref[:at]
	}
	lastSlash := strings.LastIndex(ref, "/")
	lastColon := strings.LastIndex(ref, ":")
	if lastColon > lastSlash {
		ref = ref[:lastColon]
	}
	return ref
}

func DefaultReleaseImageDigestSources(cluster ContainerCluster, mirrorURL string) []ImageDigestSource {
	mirrorURL = strings.TrimRight(strings.TrimSpace(mirrorURL), "/")
	if mirrorURL == "" {
		return nil
	}
	if source := ReleaseImageSource(cluster); source != "" {
		sources := []ImageDigestSource{{
			Source:       source,
			Mirrors:      []string{mirrorURL + "/" + DefaultMirroredReleasePath},
			SourcePolicy: ImageSourcePolicyNever,
		}}
		if source == OCPReleaseSourceQuayOCPRelease {
			sources = append(sources, ImageDigestSource{
				Source:       OCPReleaseSourceQuayARTDev,
				Mirrors:      []string{mirrorURL + "/" + DefaultMirroredReleasePath},
				SourcePolicy: ImageSourcePolicyNever,
			})
		}
		return sources
	}
	if DistributionType(cluster) != DistributionOpenShift {
		return nil
	}
	return []ImageDigestSource{
		{
			Source:       OCPReleaseSourceQuayOCPRelease,
			Mirrors:      []string{mirrorURL + "/" + DefaultMirroredReleasePath},
			SourcePolicy: ImageSourcePolicyNever,
		},
		{
			Source:       OCPReleaseSourceQuayARTDev,
			Mirrors:      []string{mirrorURL + "/" + DefaultMirroredReleasePath},
			SourcePolicy: ImageSourcePolicyNever,
		},
	}
}

func BoolPtr(v bool) *bool { return &v }

func StandardLoadBalancerPorts(endpoint string) [][2]int {
	switch endpoint {
	case EndpointAPI:
		return [][2]int{{6443, 6443}}
	case EndpointAPIInt:
		return [][2]int{{22623, 22623}}
	case EndpointIngress:
		return [][2]int{{80, 80}, {443, 443}}
	default:
		return nil
	}
}

func StandardEndpointBackendRole(endpoint string) string {
	switch endpoint {
	case EndpointAPI, EndpointAPIInt:
		return NodeRoleMaster
	default:
		return ""
	}
}

func DNSServiceIP(bind string, network NetworkConfig) string {
	if bind != "" && bind != "0.0.0.0" && bind != "::" {
		if ip := net.ParseIP(bind); ip != nil {
			for _, mn := range network.Spec.MachineNetwork {
				if _, cidr, err := net.ParseCIDR(mn.CIDR); err == nil && cidr.Contains(ip) {
					return bind
				}
			}
		}
	}
	return ""
}

func Provisioners() []string {
	return []string{
		ProvisionerLibvirt,
		ProvisionerVSphere,
		ProvisionerKubeVirt,
		ProvisionerBareMetal,
	}
}
