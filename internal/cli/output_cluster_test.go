package cli

import (
	"strings"

	"github.com/crmarques/bootwright/internal/environment/inspection"
	"github.com/crmarques/bootwright/internal/secrets"
)

const clusterDashboardPassword = "Xq7-Lm2_Rv9.Tk4-Wz8_Hn3.Pb6-Jd5_"

const (
	clusterNotImplementedSessions = "sessions on an OpenShift or OKD ContainerCluster are not yet implemented; the cluster is reached through bootwright cluster kubeconfig"
	clusterNotImplementedHandoff  = "this handoff is not yet implemented; the cluster is reached through bootwright cluster kubeconfig"
	clusterContainerOnly          = "applies to OpenShift and OKD ContainerClusters only"
	clusterManagedCephOnly        = "applies to managed Ceph StorageClusters only"
)

func containerAccess(kubeconfig inspection.AccessCommand) []inspection.AccessCommand {
	kubeconfig.Command = inspection.CommandKubeconfig
	return []inspection.AccessCommand{
		{Command: inspection.CommandExec, Status: inspection.StatusNotImplemented, Reason: clusterNotImplementedSessions},
		{Command: inspection.CommandKeyring, Status: inspection.StatusNotApplicable, Reason: clusterManagedCephOnly},
		kubeconfig,
		{Command: inspection.CommandKubectl, Status: inspection.StatusNotImplemented, Reason: clusterNotImplementedHandoff},
		{Command: inspection.CommandOC, Status: inspection.StatusNotImplemented, Reason: clusterNotImplementedHandoff},
		{Command: inspection.CommandRsh, Status: inspection.StatusNotImplemented, Reason: clusterNotImplementedSessions},
	}
}

func cephAccess(session inspection.AccessCommand, ceph *inspection.ManagedCeph) []inspection.AccessCommand {
	exec, rsh := session, session
	exec.Command, rsh.Command = inspection.CommandExec, inspection.CommandRsh
	keyring := inspection.AccessCommand{Command: inspection.CommandKeyring, Status: inspection.StatusUnavailable, Reason: "the StorageCluster storage declares no StorageClient"}
	for _, resource := range ceph.Resources {
		if resource.Kind == "StorageClient" && resource.Status == inspection.ResourceDone {
			keyring = inspection.AccessCommand{Command: inspection.CommandKeyring, Status: inspection.StatusReady}
		}
	}
	return []inspection.AccessCommand{
		exec,
		keyring,
		{Command: inspection.CommandKubeconfig, Status: inspection.StatusNotApplicable, Reason: clusterContainerOnly},
		{Command: inspection.CommandKubectl, Status: inspection.StatusNotApplicable, Reason: clusterContainerOnly},
		{Command: inspection.CommandOC, Status: inspection.StatusNotApplicable, Reason: clusterContainerOnly},
		rsh,
	}
}

func externalAccess() []inspection.AccessCommand {
	roster := "the external StorageCluster archive declares no node roster; sessions apply to managed Ceph StorageClusters"
	return []inspection.AccessCommand{
		{Command: inspection.CommandExec, Status: inspection.StatusNotApplicable, Reason: roster},
		{Command: inspection.CommandKeyring, Status: inspection.StatusNotApplicable, Reason: clusterManagedCephOnly},
		{Command: inspection.CommandKubeconfig, Status: inspection.StatusNotApplicable, Reason: clusterContainerOnly},
		{Command: inspection.CommandKubectl, Status: inspection.StatusNotApplicable, Reason: clusterContainerOnly},
		{Command: inspection.CommandOC, Status: inspection.StatusNotApplicable, Reason: clusterContainerOnly},
		{Command: inspection.CommandRsh, Status: inspection.StatusNotApplicable, Reason: roster},
	}
}

func clusterListResult() *inspection.ListResult {
	return &inspection.ListResult{Context: "lab", Clusters: []inspection.ClusterRef{
		{Name: "archive", Kind: inspection.KindStorageCluster},
		{Name: "edge", Kind: inspection.KindContainerCluster},
		{Name: "okd-lab", Kind: inspection.KindContainerCluster},
		{Name: "storage", Kind: inspection.KindStorageCluster},
	}}
}

func readyKubeconfig() inspection.AccessCommand {
	return inspection.AccessCommand{Status: inspection.StatusReady}
}

func missingKubeconfig() inspection.AccessCommand {
	return inspection.AccessCommand{Status: inspection.StatusUnavailable, Reason: "this context holds no administrator kubeconfig for okd-lab until an apply proves its installation complete; run bootwright apply --context lab"}
}

func readyCeph() *inspection.ManagedCeph {
	const fsid = "6b8f1c2e-4d7a-5e3f-9a1b-0c2d4e6f8a1b"
	return &inspection.ManagedCeph{
		Management: inspection.ManagementManaged, Release: "9.9.1.2", FSID: fsid, Status: inspection.StatusReady,
		Image: &inspection.Image{
			Reference: "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9:v9.9.1-25074",
			Digest:    "sha256:4c1f8e2b9d7a6c5e3f0b1a2d4e6f8a0c2b4d6e8f0a1c3e5b7d9f1a3c5e7b9d0f", Platform: "linux/amd64",
		},
		Monitors: []inspection.Monitor{
			{Host: "rhel-01", FQDN: "rhel-01.lab.test", Address: "192.0.2.11"},
			{Host: "rhel-02", FQDN: "rhel-02.lab.test", Address: "192.0.2.12"},
			{Host: "rhel-03", FQDN: "rhel-03.lab.test", Address: "192.0.2.13"},
		},
		AdminNode: "rhel-01",
		CephCommand: "bootwright cluster exec --context lab --name storage --node rhel-01 -- sudo -n ceph --conf /var/lib/bootwright/ceph/" + fsid +
			"/ceph.conf --keyring /var/lib/bootwright/ceph/" + fsid + "/ceph.client.admin.keyring",
		Dashboard: &inspection.Dashboard{
			Status: inspection.DashboardReady, User: "admin", CertificateSHA256: "ab" + strings.Repeat("0c", 31),
			URLs: []inspection.DashboardURL{
				{Host: "rhel-01", URL: "https://rhel-01.lab.test:8443/", Role: inspection.DashboardActive},
				{Host: "rhel-02", URL: "https://rhel-02.lab.test:8443/", Role: inspection.DashboardStandby},
			},
		},
		Resources: []inspection.Resource{
			{Kind: "StoragePool", Name: "rbd-data", Status: inspection.ResourceDone, Scope: "replicated size 2/min 2, application rbd"},
			{Kind: "StorageClient", Name: "rbd-canary", Status: inspection.ResourceDone, Scope: "client.rbd-canary read-write on pool rbd-data"},
			{Kind: "StorageObjectGateway", Name: "objects", Status: inspection.ResourceUnsupported, Scope: "zone lab/lab/lab-a"},
		},
		Endpoints: []inspection.Endpoint{
			{Service: "mgmt-gateway", URLs: []string{"https://192.0.2.50:9443/"}, CertificateSHA256: "cd" + strings.Repeat("0e", 31), Issuer: inspection.IssuerCephadm, Status: inspection.EndpointReady},
			{Service: "nfs.files", URLs: []string{"nfs://192.0.2.60:2049/"}, Status: inspection.EndpointReady},
			{Service: "rgw.objects", URLs: []string{"https://objects.lab.test:8443/"}, Issuer: inspection.IssuerBound, Status: inspection.EndpointUnproved,
				Reason: "the apply did not prove this endpoint reachable"},
		},
	}
}

func revealedCeph(unproved bool) *inspection.ManagedCeph {
	ceph := readyCeph()
	ceph.Dashboard.Password = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(clusterDashboardPassword)})
	ceph.Dashboard.Revealed = true
	if unproved {
		ceph.Dashboard.Status, ceph.Dashboard.Warn = inspection.DashboardUnproved, true
		ceph.Dashboard.Reason = "the apply's controller-side TLS connect to the active manager's dashboard port did not present the stored certificate"
	}
	return ceph
}

func storageOnly(ceph *inspection.ManagedCeph) *inspection.InfoResult {
	return &inspection.InfoResult{Context: "lab", Clusters: []inspection.ContainerEntry{}, Storage: []inspection.StorageEntry{
		{Name: "storage", Kind: inspection.KindStorageCluster, AccessCommands: cephAccess(inspection.AccessCommand{Status: inspection.StatusReady, Node: "rhel-01"}, ceph), Ceph: ceph},
	}}
}

func unavailableCeph() *inspection.ManagedCeph {
	return &inspection.ManagedCeph{
		Management: inspection.ManagementManaged, Release: "9.9.1.2", AdminNode: "rhel-01", Status: inspection.StatusUnavailable,
		Reason:    "no apply of this context's current operation has completed the cluster's bootstrap",
		Remedy:    "bootwright apply --context lab --stage clusters",
		Resources: []inspection.Resource{{Kind: "StoragePool", Name: "rbd-data", Status: inspection.ResourcePending}},
	}
}

func clusterInfoResult(ceph *inspection.ManagedCeph, session inspection.AccessCommand, kubeconfig inspection.AccessCommand) *inspection.InfoResult {
	return &inspection.InfoResult{
		Context: "lab",
		Clusters: []inspection.ContainerEntry{
			{Name: "edge", Kind: inspection.KindContainerCluster, AccessCommands: containerAccess(readyKubeconfig())},
			{Name: "okd-lab", Kind: inspection.KindContainerCluster, AccessCommands: containerAccess(kubeconfig)},
		},
		Storage: []inspection.StorageEntry{
			{Name: "archive", Kind: inspection.KindStorageCluster, AccessCommands: externalAccess()},
			{Name: "storage", Kind: inspection.KindStorageCluster, AccessCommands: cephAccess(session, ceph), Ceph: ceph},
		},
	}
}

func clusterGoldens() []cliGolden {
	ready := func(r *dispatchRecord) {
		r.result.clusterInfo = clusterInfoResult(readyCeph(), inspection.AccessCommand{Status: inspection.StatusReady, Node: "rhel-01"}, readyKubeconfig())
	}
	missing := func(r *dispatchRecord) {
		r.result.clusterInfo = clusterInfoResult(unavailableCeph(),
			inspection.AccessCommand{Status: inspection.StatusUnavailable, Node: "rhel-01", Reason: "the Machine rhel-01, which runs node rhel-01, has no proved host key; bootwright apply --context lab --stage machines records it"},
			missingKubeconfig())
	}
	list := func(r *dispatchRecord) { r.result.clusterList = clusterListResult() }
	revealed := func(r *dispatchRecord) { r.result.clusterInfo = storageOnly(revealedCeph(false)) }
	unproved := func(r *dispatchRecord) { r.result.clusterInfo = storageOnly(revealedCeph(true)) }
	warning := "  warning  access.unproved\n" +
		"    the dashboard administrator password of StorageCluster storage was kept from an apply that\n" +
		"    never proved the dashboard reachable, so the access it grants was not proved\n"
	return []cliGolden{
		{golden: "cli-cluster-list", args: "cluster list", record: list},
		{golden: "cli-cluster-list-json", args: "cluster list --output json", record: list},
		{golden: "cli-cluster-info", args: "cluster info", record: ready},
		{golden: "cli-cluster-info-json", args: "cluster info --output json", record: ready},
		{golden: "cli-cluster-info-full", args: "cluster info --detail full", record: ready},
		{golden: "cli-cluster-info-unavailable", args: "cluster info", record: missing},
		{golden: "cli-cluster-info-unavailable-json", args: "cluster info --output json", record: missing},
		{golden: "cli-cluster-info-secrets", args: "cluster info --name storage --secrets", record: revealed},
		{golden: "cli-cluster-info-secrets-json", args: "cluster info --name storage --secrets --output json", record: revealed},
		{golden: "cli-cluster-info-unproved", args: "cluster info --name storage --secrets", record: unproved, stderr: warning},
		{golden: "cli-cluster-info-unproved-json", args: "cluster info --name storage --secrets --output json", record: unproved},
		{golden: "cli-cluster-keyring", args: "cluster keyring --name storage --client vm-disks", quoted: true, record: func(r *dispatchRecord) { r.result.keyring = keyringResult(false) }},
		{golden: "cli-cluster-keyring-json", args: "cluster keyring --name storage --client vm-disks --output json", record: func(r *dispatchRecord) { r.result.keyring = keyringResult(false) }},
		{
			args: "cluster keyring --name storage --client missing", code: 1, record: func(r *dispatchRecord) { r.err = keyringTargetRefusal },
			stderr: "  error  access.target\n" +
				"    the context lab selects no StorageClient named missing\n" +
				"    Next: bootwright cluster info --context lab --name storage lists the StorageClients of\n" +
				"    storage\n",
		},
	}
}
