package containercluster

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/environment"
)

// releaseTopology is the cluster shape one release's agent installer accepts,
// read from that release's openshift/installer branch. The release's own
// install-config validation only refuses zero control-plane replicas; the
// agent installer narrows that to the counts listed here.
type releaseTopology struct {
	// controlPlane lists the master counts the agent installer accepts. Two
	// is accepted upstream only beside an arbiter pool or, from release-4.22,
	// two fencing credentials, and a node role here is never an arbiter and
	// carries no fencing, so no rule lists it.
	controlPlane []int
	// sources are the upstream files and lines the rule was read from.
	sources []string
}

var generalTopology = releaseTopology{
	controlPlane: []int{1, 3, 4, 5},
	sources: []string{
		"https://github.com/openshift/installer/blob/release-4.21/pkg/asset/agent/installconfig.go#L217-L232",
		"https://github.com/openshift/installer/blob/release-4.21/pkg/asset/agent/installconfig.go#L234-L263",
		"https://github.com/openshift/installer/blob/release-4.21/pkg/types/validation/installconfig.go#L758-L771",
		"https://github.com/openshift/installer/blob/release-4.22/pkg/asset/agent/installconfig.go#L220-L305",
		"https://github.com/openshift/installer/blob/release-4.22/pkg/asset/agent/installconfig.go#L307-L336",
		"https://github.com/openshift/installer/blob/release-4.22/pkg/types/validation/installconfig.go#L1731-L1753",
		"https://github.com/openshift/installer/blob/release-4.23/pkg/asset/agent/installconfig.go#L220-L305",
		"https://github.com/openshift/installer/blob/release-4.23/pkg/asset/agent/installconfig.go#L307-L336",
		"https://github.com/openshift/installer/blob/release-4.23/pkg/types/validation/installconfig.go#L1803-L1825",
	},
}

// validateTopology checks the node roster and endpoint ownership against what
// the declared release installs. It runs on effective state, where the
// platform is already derived, and never on a partial fragment.
func validateTopology(s api.Value, partial bool) []api.Issue {
	if partial {
		return nil
	}
	issues := []api.Issue{}
	version := s.Get("distribution", "release", "version").Text()
	minor, ok := releaseMinor(version)
	if !ok {
		minor = version
	}
	nodes := s.Get("nodes").Items()
	if len(nodes) == 0 {
		return issues
	}
	masters, compute := 0, 0
	for _, node := range nodes {
		switch node.Get("role").Text() {
		case "master":
			masters++
		case "worker", "infra":
			compute++
		}
	}
	if masters == 0 {
		return add(issues, invariantRemediation("$.spec.nodes", "a container cluster requires at least one master node", "declare at least one master in spec.nodes"))
	}
	if version != "" && !slices.Contains(generalTopology.controlPlane, masters) {
		accepted := countList(generalTopology.controlPlane)
		issue := invariant("$.spec.nodes", fmt.Sprintf("release %s accepts %s master nodes, not %d", minor, accepted, masters))
		issue.Remediation = "declare " + accepted + " master nodes"
		issues = add(issues, issue)
	}
	if masters == 1 && compute > 0 {
		issue := invariant("$.spec.nodes", fmt.Sprintf("a single-master cluster admits no worker or infra node, and %d are declared", compute))
		issue.Remediation = "remove the worker and infra nodes, or declare as many master nodes as the release accepts"
		issues = add(issues, issue)
	}
	if len(nodes) > 1 && s.Get("install", "platform", "type").Text() == "none" {
		for _, slot := range endpointSlots {
			endpoint := s.Get("install", "endpoints", slot)
			if endpoint.Present() && sourceType(endpoint) == "openshift" {
				issue := invariant("$.spec.install.endpoints."+slot+".source.type", "a multi-node cluster on platform none installs no endpoint VIPs, so the installer cannot own this endpoint")
				issue.Remediation = "declare an external or loadBalancer source for this endpoint"
				issues = add(issues, issue)
			}
		}
	}
	return issues
}

// releaseMinor is a release version's numeric major and minor, as in 4.21 for
// 4.21.15.
func releaseMinor(version string) (string, bool) {
	parts := strings.Split(version, ".")
	if len(parts) < 2 || !decimal(parts[0]) || !decimal(parts[1]) {
		return "", false
	}
	return parts[0] + "." + parts[1], true
}

// countList renders counts as a sentence list: 1, 3, 4 or 5.
func countList(counts []int) string {
	words := make([]string, len(counts))
	for i, count := range counts {
		words[i] = strconv.Itoa(count)
	}
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " or " + words[len(words)-1]
}

func MemberMachines(object api.Object) (environment.Membership, bool) {
	if object.Kind() != api.ContainerCluster {
		return environment.Membership{}, false
	}
	return environment.MembersOf("$.spec.nodes", object.Spec().Get("nodes")), true
}
