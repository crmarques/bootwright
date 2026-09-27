package containercluster

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// releaseTopology is the cluster shape one release's agent installer accepts,
// read from that release's openshift/installer branch. The release's own
// install-config validation only refuses zero control-plane replicas; the
// agent installer narrows that to the counts listed here.
type releaseTopology struct {
	// controlPlane lists the master counts the agent installer accepts. Two
	// is accepted upstream only beside an arbiter pool, which needs a feature
	// gate (pkg/types/validation/installconfig.go#L138-L144 on release-4.21),
	// and a node role here is never an arbiter, so no row lists it.
	controlPlane []int
	// sources are the upstream files and lines the row was read from.
	sources []string
}

// releaseTopologies holds one row per release minor Bootwright qualifies a
// topology for. A release with no row refuses at admission, so qualifying a
// new minor means reading its branch and adding its row here and in the spec.
// Every row's installer also refuses a compute node beside a single master,
// which validateTopology applies to every release. The 4.21 lines were read
// from the release-4.21 branch on 2026-09-27.
var releaseTopologies = map[string]releaseTopology{
	"4.21": {
		controlPlane: []int{1, 3, 4, 5},
		sources: []string{
			"https://github.com/openshift/installer/blob/release-4.21/pkg/asset/agent/installconfig.go#L217-L232",
			"https://github.com/openshift/installer/blob/release-4.21/pkg/asset/agent/installconfig.go#L234-L263",
			"https://github.com/openshift/installer/blob/release-4.21/pkg/types/validation/installconfig.go#L758-L771",
		},
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
	// A release pinned by image alone declares no minor to look up; the
	// container-cluster selection refuses it before anything is planned.
	row, qualified := releaseTopologies[minor]
	if version != "" && !qualified {
		issue := invariant("$.spec.distribution.release.version", "the topology table qualifies no release "+minor)
		issue.Remediation = "declare a release version in a minor the topology table qualifies: " + strings.Join(qualifiedMinors(), ", ")
		issues = add(issues, issue)
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
		return add(issues, invariant("$.spec.nodes", "a container cluster requires at least one master node"))
	}
	if qualified && !slices.Contains(row.controlPlane, masters) {
		accepted := countList(row.controlPlane)
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

func qualifiedMinors() []string {
	minors := make([]string, 0, len(releaseTopologies))
	for minor := range releaseTopologies {
		minors = append(minors, minor)
	}
	slices.Sort(minors)
	return minors
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
