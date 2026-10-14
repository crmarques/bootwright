package ceph

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage"
)

type ImageInspection struct {
	Reference string
	Digest    string
	Platform  string
}

type MonitorInspection struct {
	Host    string
	FQDN    string
	Address string
}

type DashboardURL struct {
	Host string
	URL  string
	Role string
}

type DashboardInspection struct {
	Status            string
	Reason            string
	User              string
	URLs              []DashboardURL
	CertificateSHA256 string
}

type Inspection struct {
	FSID      string
	Image     ImageInspection
	Monitors  []MonitorInspection
	Dashboard DashboardInspection
}

const (
	DashboardReady    = "ready"
	DashboardUnproved = "unproved"
	DashboardActive   = "active"
	DashboardStandby  = "standby"
)

const (
	noClusterProof = "no apply of this context's current operation has completed the cluster's bootstrap"
	noImageProof   = "no apply of this context's current operation has recorded the cluster's resolved Ceph image"
)

func Inspect(catalog api.Catalog, cluster api.Object, proofs []lifecycle.BlockEvidence) (Inspection, string) {
	image, reason := inspectImage(cluster.Name(), proofs)
	if reason != "" {
		return Inspection{}, reason
	}
	evidence, reason := inspectCluster(cluster.Name(), proofs)
	if reason != "" {
		return Inspection{}, reason
	}
	monitors, reason := inspectMonitors(catalog, cluster, evidence.Monitors, evidence.MonAddrs)
	if reason != "" {
		return Inspection{}, reason
	}
	dashboard, reason := inspectDashboard(cluster, evidence)
	if reason != "" {
		return Inspection{}, reason
	}
	return Inspection{FSID: evidence.FSID, Image: image, Monitors: monitors, Dashboard: dashboard}, ""
}

func inspectDashboard(cluster api.Object, evidence Evidence) (DashboardInspection, string) {
	nodes := cluster.Spec().Get("ceph", "topology", "nodes").Items()
	node := func(native string) (string, bool) {
		index := slices.IndexFunc(nodes, func(node api.Value) bool { return storage.NativeHostname(node) == native })
		if index < 0 {
			return "", false
		}
		return nodes[index].Get("name").Text(), true
	}
	d := evidence.Dashboard
	result := DashboardInspection{Status: DashboardReady, User: DashboardUser, URLs: []DashboardURL{}, CertificateSHA256: d.CertificateSHA256}
	if d.URL != "" {
		active, ok := node(evidence.MGRActive)
		if !ok {
			return DashboardInspection{}, "the active manager in the cluster's evidence names no declared node"
		}
		result.URLs = append(result.URLs, DashboardURL{Host: active, URL: d.URL, Role: DashboardActive})
		for _, standby := range evidence.MGRStandbys {
			name, ok := node(standby)
			if !ok {
				return DashboardInspection{}, "a standby manager in the cluster's evidence names no declared node"
			}
			result.URLs = append(result.URLs, DashboardURL{Host: name, URL: "https://" + standby + ":" + DashboardPort + "/", Role: DashboardStandby})
		}
	}
	switch {
	case d.URL == "" || d.CertificateSHA256 == "":
		result.Status, result.Reason = DashboardUnproved, "the apply did not observe the active manager serving the dashboard with a stored certificate"
	case !d.Reachable:
		result.Status, result.Reason = DashboardUnproved, "the apply's controller-side TLS connect to the active manager's dashboard port did not present the stored certificate"
	}
	return result, ""
}

func appliedProof(proofs []lifecycle.BlockEvidence, cluster, implementation string) (lifecycle.BlockEvidence, int) {
	var found lifecycle.BlockEvidence
	count := 0
	for _, proof := range proofs {
		if proof.Kind != string(api.StorageCluster) || proof.Object != cluster || proof.Implementation != implementation ||
			proof.Verb != reconciliation.Apply || proof.State != reconciliation.BlockDone {
			continue
		}
		found = proof
		count++
	}
	return found, count
}

func inspectImage(cluster string, proofs []lifecycle.BlockEvidence) (ImageInspection, string) {
	proof, count := appliedProof(proofs, cluster, ImageImplementation)
	if count == 0 {
		return ImageInspection{}, noImageProof
	}
	if count > 1 {
		return ImageInspection{}, "the cluster's resolved Ceph image has ambiguous evidence"
	}
	evidence, err := reconciliation.DecodeEvidence[ImageEvidence](proof.Evidence, maxImageEvidence, "Ceph image")
	if err != nil || evidence.Absent || !evidence.Postcondition || evidence.Release != PinnedRelease || evidence.Base != PinnedRepository ||
		evidence.Tag == "" || evidence.Platform != PinnedPlatform || !ValidImageDigest(evidence.Digest) {
		return ImageInspection{}, "the cluster's resolved Ceph image evidence is not the pinned linux/amd64 manifest"
	}
	return ImageInspection{Reference: evidence.Base + ":" + evidence.Tag, Digest: evidence.Digest, Platform: evidence.Platform}, ""
}

func inspectCluster(cluster string, proofs []lifecycle.BlockEvidence) (Evidence, string) {
	proof, count := appliedProof(proofs, cluster, Implementation)
	if count == 0 {
		return Evidence{}, noClusterProof
	}
	if count > 1 {
		return Evidence{}, "the cluster's bootstrap has ambiguous evidence"
	}
	evidence, err := decodeEvidence(proof.Evidence)
	if err != nil || evidence.Version != EvidenceVersion || evidence.Action != "cluster" || !evidence.Ready || !evidence.Owned || !evidence.Exists ||
		evidence.Partial || evidence.Removed || evidence.FSID == "" || len(evidence.Monitors) == 0 {
		return Evidence{}, "the cluster's bootstrap evidence does not prove a ready cluster"
	}
	return evidence, ""
}

func inspectMonitors(catalog api.Catalog, cluster api.Object, names []string, addresses []MonitorAddress) ([]MonitorInspection, string) {
	nodes := cluster.Spec().Get("ceph", "topology", "nodes").Items()
	monitors := make([]MonitorInspection, 0, len(names))
	for _, native := range names {
		index := slices.IndexFunc(nodes, func(node api.Value) bool { return storage.NativeHostname(node) == native })
		if index < 0 {
			return nil, "a monitor in the cluster's bootstrap evidence names no declared node"
		}
		node := nodes[index]
		host, _ := catalog.Find(api.Machine, node.Get("machineRef").Text())
		ssh, ok := machine.SSH(host)
		if !ok {
			return nil, "a monitor's node declares no Machine contact address"
		}
		address := ssh.Address
		if index := slices.IndexFunc(addresses, func(m MonitorAddress) bool { return m.Host == native }); index >= 0 {
			address = addresses[index].Address
		}
		monitors = append(monitors, MonitorInspection{Host: node.Get("name").Text(), FQDN: native, Address: address})
	}
	slices.SortFunc(monitors, func(a, b MonitorInspection) int { return strings.Compare(a.Host, b.Host) })
	return monitors, ""
}
