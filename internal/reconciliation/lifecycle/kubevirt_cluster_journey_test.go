package lifecycle_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/containercluster/openshift"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/material"
	"github.com/crmarques/bootwright/internal/substrate/kubevirt"
)

const kubevirtHostUID = "0b1c2d3e-0000-4000-8000-000000000001"

var kubevirtNodes = []string{"kubevirt-01", "kubevirt-02", "kubevirt-03"}

func kubevirtJourneyField(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}

func kubevirtJourneyText(name, value string) api.FieldValue {
	return kubevirtJourneyField(name, api.StringValue(value))
}

// kubevirtClusterCatalog is lab-kubevirt-external's shape: three control-plane
// KubeVirt Machines on an external host reached through kubeconfigRef, on the
// derived none platform behind external endpoints.
func kubevirtClusterCatalog() api.Catalog {
	f, s := kubevirtJourneyField, kubevirtJourneyText
	objects := []api.Object{
		api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
			f("domains", api.MapValue(s("base", "lab.example.test"), s("machines", "lab.example.test"),
				s("clusters", "lab.example.test"), s("containerClusters", "lab.example.test"))), f("controller", api.MapValue(s("machineRef", "controller"))))),
		api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue(
			f("os", api.MapValue(f("provided", api.BoolValue(true)))),
			f("network", api.MapValue(f("addresses", api.ListValue(api.MapValue(s("name", "ip"), s("address", "192.0.2.1")))))),
			f("access", api.MapValue(f("local", api.BoolValue(true)))))),
		api.NewObject(api.NetworkConfig, "lab-guests", api.Value{}, api.MapValue(
			f("machineNetwork", api.ListValue(api.MapValue(s("cidr", "198.51.100.0/24")))),
			f("nmstate", api.MapValue(
				f("interfaces", api.ListValue(api.MapValue(s("name", "enp1s0"), s("type", "ethernet"), s("state", "up"),
					f("ipv4", api.MapValue(f("enabled", api.BoolValue(true)), f("dhcp", api.BoolValue(false))))))),
				f("routes", api.MapValue(f("config", api.ListValue(api.MapValue(s("destination", "0.0.0.0/0"),
					s("next-hop-address", "198.51.100.1"), s("next-hop-interface", "enp1s0")))))))))),
		api.NewObject(api.InfraProvider, "lab-kubevirt", api.Value{}, api.MapValue(
			f("kubevirt", api.MapValue(s("kubeconfigRef", "host-kubeconfig"), s("namespace", "bootwright-guests"), s("storageClassRef", "guest-block-storage"),
				f("machineProfiles", api.ListValue(api.MapValue(s("name", "kubevirt"), f("cpu", api.IntegerValue("6")),
					f("memoryMiB", api.IntegerValue("16384")), f("diskGiB", api.IntegerValue("120"))))))),
			f("networkAttachments", api.ListValue(api.MapValue(s("name", "lab-guests"), f("kubevirt", api.MapValue(f("networkRef", api.MapValue(
				s("apiGroup", "k8s.cni.cncf.io"), s("kind", "NetworkAttachmentDefinition"), s("namespace", "bootwright-guests"), s("name", "guest-l2")))))))))),
	}
	var nodes []api.Value
	for index, name := range kubevirtNodes {
		objects = append(objects, api.NewObject(api.Machine, name, api.Value{}, api.MapValue(
			f("substrate", api.MapValue(s("providerRef", "lab-kubevirt"), f("kubevirt", api.MapValue(s("profileRef", "kubevirt"))))),
			f("hardware", api.MapValue(f("rootDevice", api.MapValue(s("deviceName", "/dev/vda"))))),
			f("os", api.MapValue(f("provided", api.BoolValue(false)))),
			f("network", api.MapValue(s("configRef", "lab-guests"), s("attachmentRef", "lab-guests"), s("installAddressRef", "ip"),
				f("addresses", api.ListValue(api.MapValue(s("name", "ip"), s("address", "198.51.100.2"+string(rune('1'+index))+"/24"), s("interface", "enp1s0")))))))))
		nodes = append(nodes, api.MapValue(s("name", "master-"+string(rune('0'+index))), s("role", "controlPlane"), s("machineRef", name)))
	}
	endpoint := func(address string) api.Value {
		return api.MapValue(s("address", address), f("source", api.MapValue(s("type", "external"))))
	}
	cluster := api.NewObject(api.OpenShiftCluster, "kubevirt", api.Value{}, api.MapValue(
		f("distribution", api.MapValue(s("type", "openshift"), f("release", api.MapValue(s("version", installedRelease))))),
		f("install", api.MapValue(s("method", "agent"), s("mode", "connected"), s("pullSecretRef", "openshift-pull-secret"),
			f("nodeSSH", api.MapValue(s("keyPairRef", "kubevirt-cluster-admin-ssh-key"))), f("agent", api.MapValue()),
			f("endpoints", api.MapValue(f("api", endpoint("198.51.100.10")), f("api-int", endpoint("198.51.100.10")), f("ingress", endpoint("198.51.100.11")))))),
		f("nodes", api.ListValue(nodes...))))
	normalized, _ := openshift.Normalize(cluster, api.NewCatalog(objects))
	return api.NewCatalog(append(objects, normalized))
}

// kubevirtHost is the external OpenShift Virtualization host and the agent
// installer, as every adapter reads and changes them: a VirtualMachine per
// realized Machine, the upload DataVolume the cluster's image is pushed to,
// the claim that outlives it until the instance next stops, and the cluster
// those machines install. failAt interrupts the named step once, after its
// effect.
type kubevirtHost struct {
	mutex      sync.Mutex
	machines   map[string]string
	running    map[string]bool
	uploaded   map[string]bool
	claims     map[string]bool
	inserts    map[string]int
	installed  bool
	released   bool
	failAt     string
	calls      []string
	pinRefused []string
	// nested hosts the provider on a cluster of the context: every run is
	// then lent a token the lend minted for it.
	nested      bool
	unreachable bool
	minted      int
	tokens      []string
	lendRefused []string
}

func newKubevirtHost() *kubevirtHost {
	return &kubevirtHost{machines: map[string]string{}, running: map[string]bool{}, uploaded: map[string]bool{},
		claims: map[string]bool{}, inserts: map[string]int{}}
}

func (h *kubevirtHost) interrupted(step string) bool {
	if h.failAt != step {
		return false
	}
	h.failAt = ""
	return true
}

func (h *kubevirtHost) Run(_ context.Context, run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	h.calls = append(h.calls, run.Implementation+" "+run.Operation)
	if run.Operation == "lend" {
		return h.lend(run)
	}
	if h.nested && run.Implementation != agentinstall.MediaImplementation {
		if err := h.mintedFor(run); err != nil {
			h.lendRefused = append(h.lendRefused, err.Error())
			return lifecycle.RunResult{}, err
		}
	}
	switch run.Implementation {
	case kubevirt.HostImplementation:
		return h.proof(run)
	case kubevirt.MachineImplementation:
		return h.machine(run)
	case agentinstall.MediaImplementation:
		return h.media(run)
	}
	return h.install(run)
}

func (h *kubevirtHost) proof(run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	request, err := kubevirt.DecodeHostRequest(run.Canonical)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	if run.Operation == "destroy" {
		return kubevirtResult("unchanged", kubevirt.HostEvidence{Absent: true, Failures: []string{}, Networks: []kubevirt.NetworkEvidence{},
			Postcondition: true, Request: run.Digest})
	}
	networks := []kubevirt.NetworkEvidence{}
	for _, network := range request.Networks {
		networks = append(networks, kubevirt.NetworkEvidence{Attachment: network.Attachment, Present: true})
	}
	return kubevirtResult("unchanged", kubevirt.HostEvidence{Failures: []string{}, KubeSystemUID: kubevirtHostUID, MACPool: "outside",
		Networks: networks, Postcondition: true, ReadWriteMany: true, Request: run.Digest, Server: "https://api.host.example.test:6443",
		StorageClass: "guest-block-storage", UploadProxy: "https://cdi-uploadproxy.apps.host.example.test", Version: "v1.34.2"})
}

func (h *kubevirtHost) machine(run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	request, err := kubevirt.DecodeMachineRequest(run.Canonical)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	name := request.Identity.Object
	if run.Operation == "apply" && h.machines[name] == "" {
		h.machines[name] = "6a1f0000-0000-4000-8000-0000000000" + name[len(name)-2:]
	}
	if run.Operation == "destroy" {
		if h.running[name] {
			return lifecycle.RunResult{}, errors.New("the VirtualMachineInstance of " + name + " is running")
		}
		delete(h.machines, name)
		delete(h.uploaded, name)
		delete(h.claims, name)
	}
	evidence := kubevirt.MachineEvidence{Answered: true, KubeSystemUID: kubevirtHostUID, Request: run.Digest, Server: "https://api.host.example.test:6443",
		Volumes: []kubevirt.VolumeEvidence{{Kind: "DataVolume", Name: request.RootVolume}, {Kind: "DataVolume", Name: request.MediaVolume, Present: h.uploaded[name]}}}
	if h.claims[name] {
		evidence.Volumes = append(evidence.Volumes, kubevirt.VolumeEvidence{Kind: "PersistentVolumeClaim", Name: request.MediaVolume, Present: true})
	}
	if uid, ok := h.machines[name]; ok {
		evidence.Owned, evidence.Postcondition, evidence.VirtualMachine = true, true, request.Name
		evidence.Receipts = []kubevirt.Receipt{{Basis: "created", Kind: "VirtualMachine", Name: request.Name, UID: uid}}
		evidence.Volumes[0].Present = true
		if h.running[name] {
			evidence.Instance = "Running"
		}
	} else {
		evidence.Absent, evidence.Postcondition = true, !h.claims[name] && !h.uploaded[name]
	}
	outcome := "unchanged"
	if run.Operation != "observe" {
		outcome = "changed"
	}
	return kubevirtResult(outcome, evidence)
}

func (h *kubevirtHost) media(run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	request, err := agentinstall.DecodeMediaRequest(run.Canonical)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	if request.DeliveryKind != "push" {
		return lifecycle.RunResult{}, errors.New("a KubeVirt cluster's image was planned for publication")
	}
	evidence := agentinstall.MediaEvidence{Image: true, Inputs: run.Digest, Installer: request.Release.Version, Postcondition: true, Request: run.Digest, Work: true}
	if run.Operation == "destroy" {
		evidence = agentinstall.MediaEvidence{Absent: true, Postcondition: true, Request: run.Digest}
	}
	return kubevirtResult("changed", evidence)
}

// install is the agent installation's adapter: it checks that every node it
// reaches is handed the cluster and VirtualMachine identity its Machine's
// block recorded, pushes and boots a node not already running this cluster's
// image, waits, then ejects, deleting the upload DataVolume while the claim
// stays until the instance next stops.
func (h *kubevirtHost) install(run lifecycle.RunRequest) (lifecycle.RunResult, error) {
	request, err := agentinstall.DecodeInstallRequest(run.Canonical)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	encode := func(value string) string { return base64.StdEncoding.EncodeToString([]byte(value)) }
	for index, node := range request.Nodes {
		position := "Node" + string(rune('0'+index))
		if run.MaterialValues["pinnedUUIDBase64"+position] != encode(kubevirtHostUID) ||
			run.MaterialValues["pinnedSerialBase64"+position] != encode(h.machines[node.Machine]) {
			h.pinRefused = append(h.pinRefused, node.Machine)
			return lifecycle.RunResult{}, errors.New("identity-mismatch at " + node.Machine)
		}
	}
	if (run.Operation == "apply") != (run.MaterialValues["virtctl"] != "") {
		return lifecycle.RunResult{}, errors.New("virtctl is located for an apply's push alone")
	}
	for _, lent := range run.Materials {
		if lent.Secret == "" {
			return lifecycle.RunResult{}, errors.New("material " + lent.Name + " names no Secret")
		}
	}
	switch run.Operation {
	case "apply":
		if !h.installed {
			for _, node := range request.Nodes {
				if h.running[node.Machine] && h.uploaded[node.Machine] {
					continue
				}
				h.inserts[node.Machine]++
				h.uploaded[node.Machine], h.claims[node.Machine], h.running[node.Machine] = true, true, true
			}
			if h.interrupted("boot") {
				return lifecycle.RunResult{}, errors.New("the attempt was interrupted during the bootstrap wait")
			}
			h.installed = true
		}
		for _, node := range request.Nodes {
			delete(h.uploaded, node.Machine)
		}
		h.released = true
		if h.interrupted("release") {
			return lifecycle.RunResult{}, errors.New("the reply was lost after the media was released")
		}
		return kubevirtInstalled(run, request, h, true)
	case "destroy":
		for _, node := range request.Nodes {
			delete(h.uploaded, node.Machine)
		}
		if h.interrupted("eject") {
			return lifecycle.RunResult{}, errors.New("the reply was lost after every node's media was ejected")
		}
		evidence := agentinstall.InstallEvidence{Absent: true, Media: []string{}, Missing: []string{}, OwnMedia: []string{},
			Postcondition: true, Powered: []string{}, Request: run.Digest}
		return kubevirtResult("changed", evidence)
	}
	return kubevirtInstalled(run, request, h, len(run.Outputs) != 0)
}

func kubevirtInstalled(run lifecycle.RunRequest, request agentinstall.InstallRequest, h *kubevirtHost, offer bool) (lifecycle.RunResult, error) {
	evidence := agentinstall.InstallEvidence{Identity: installIdentity, Media: []string{}, Missing: []string{}, OwnMedia: []string{},
		Powered: []string{}, Request: run.Digest}
	for _, node := range request.Nodes {
		if h.uploaded[node.Machine] {
			evidence.Media, evidence.OwnMedia = append(evidence.Media, node.Machine), append(evidence.OwnMedia, node.Machine)
		}
		if h.running[node.Machine] {
			evidence.Powered = append(evidence.Powered, node.Machine)
		}
	}
	if h.installed {
		evidence.Cluster, evidence.Completed, evidence.Release, evidence.Postcondition = installIdentity, true, installedRelease, len(evidence.Media) == 0
	}
	result, err := kubevirtResult("changed", evidence)
	if offer && h.installed {
		result.Produced = adminKubeconfig()
	}
	return result, err
}

func kubevirtResult(outcome string, evidence any) (lifecycle.RunResult, error) {
	data, err := json.Marshal(evidence)
	return lifecycle.RunResult{Outcome: outcome, Evidence: data}, err
}

// recordedPins reads each KubeVirt Machine's pin from its own block's apply
// record, as composition does.
type recordedPins struct{}

func (recordedPins) PinnedIdentity(name string, proved []lifecycle.BlockEvidence) (machineref.HardwareIdentity, bool, error) {
	return kubevirt.PinnedIdentity(name, proved)
}

// locatedInstaller is the installation capability over a controller stage that
// published the release's installer and clients and the provider's virtctl.
type locatedInstaller struct{ agentinstall.InstallCapability }

func (c locatedInstaller) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.InstallCapability.Apply(ctx, located(execution))
}

func (c locatedInstaller) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.InstallCapability.Destroy(ctx, located(execution))
}

func (c locatedInstaller) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.InstallCapability.Observe(ctx, located(execution))
}

func (c locatedInstaller) ObserveRemoval(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.InstallCapability.ObserveRemoval(ctx, located(execution))
}

// locatedMedia is the media capability over the same controller stage.
type locatedMedia struct{ agentinstall.MediaCapability }

func (c locatedMedia) Apply(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.MediaCapability.Apply(ctx, located(execution))
}

func (c locatedMedia) Destroy(ctx context.Context, execution lifecycle.Execution) (lifecycle.Result, error) {
	return c.MediaCapability.Destroy(ctx, located(execution))
}

func (c locatedMedia) Observe(ctx context.Context, execution lifecycle.Execution) (lifecycle.Observation, error) {
	return c.MediaCapability.Observe(ctx, located(execution))
}

// kubevirtClusterJourney plans the provider proof, the three Machines, the
// cluster's media and its installation from one catalog, each by its own
// production capability, all over one host.
func kubevirtClusterJourney(t *testing.T) (*lifecycle.CapabilityJourney, *kubevirtHost) {
	t.Helper()
	catalog := kubevirtClusterCatalog()
	input := lifecycle.PlanInput{Verb: reconciliation.Apply, State: compilation.NewState(catalog, catalog, nil),
		Controller: "controller", Context: lifecycle.ContextIdentity{Name: lifecycle.JourneyContext}}
	host := newKubevirtHost()
	bound := []lifecycle.BoundCapability{
		{Binding: lifecycle.CapabilityBinding{Kind: kubevirt.HostKind, Implementation: kubevirt.HostImplementation}, Capability: kubevirt.NewHost(host, nil)},
		{Binding: lifecycle.CapabilityBinding{Kind: kubevirt.MachineKind, Implementation: kubevirt.MachineImplementation}, Capability: kubevirt.NewMachine(host)},
		{Binding: lifecycle.CapabilityBinding{Kind: agentinstall.Kind, Implementation: agentinstall.MediaImplementation}, Capability: locatedMedia{agentinstall.NewMedia(host, material.AuthorizedKey)}},
		{Binding: lifecycle.CapabilityBinding{Kind: agentinstall.Kind, Implementation: agentinstall.InstallImplementation},
			Capability: locatedInstaller{agentinstall.NewInstall(host).WithIdentities(recordedPins{})}},
	}
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	for _, capability := range bound {
		planned, err := capability.Capability.Plan(context.Background(), input)
		if err != nil {
			t.Fatalf("planning %s: %v", capability.Binding.Implementation, diagnostics.Of(err))
		}
		// The journey's fixed plan is contributed by its first binding, so no
		// definition names a family the engine would hold to that binding's.
		for _, definition := range planned.Definitions {
			definition.Family = ""
			plan.Definitions = append(plan.Definitions, definition)
		}
		plan.Secrets = append(plan.Secrets, planned.Secrets...)
	}
	slices.Sort(plan.Secrets)
	plan.Secrets = slices.Compact(plan.Secrets)
	return lifecycle.NewBoundJourney(t, bound, plan), host
}

// holdKubevirtSecrets gives the cluster's Secrets fresh material before each
// command, as the binder reads them anew: the host kubeconfig, the pull
// secret and the administration key pair.
func holdKubevirtSecrets(journey *lifecycle.CapabilityJourney) {
	journey.Holding("host-kubeconfig", secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("apiVersion: v1\nkind: Config\n")}))
	journey.Holding("openshift-pull-secret", secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(`{"auths":{}}`)}))
	journey.Holding("kubevirt-cluster-admin-ssh-key", secrets.NewMaterial(map[secrets.Part][]byte{
		secrets.PrivateKeyPart: []byte("cluster-private"), secrets.PublicKeyPart: append(fleetHostKey(), []byte(" cluster")...)}))
}

// The KubeVirt cluster's journey: the provider proof and the Machines precede
// the push; every entry point of every node is handed the identity its
// Machine's block recorded; an apply interrupted during the bootstrap wait, or
// after the release whose reply was lost, is continued without pushing a node
// already running this cluster's image again; the release deletes each upload
// DataVolume while its claim stays until the instance stops; and once the
// operator stops the nodes the destroy proves every VirtualMachine,
// DataVolume and claim absent.
func TestAKubeVirtClusterIsPushedBootedReleasedContinuedAndDestroyed(t *testing.T) {
	for _, interruption := range []string{"", "boot", "release", "eject"} {
		t.Run("interrupted at "+interruption, func(t *testing.T) {
			journey, host := kubevirtClusterJourney(t)
			host.failAt = interruption
			apply := lifecycle.ApplyRequest{ContextName: lifecycle.JourneyContext, SkipConfirmation: true}
			holdKubevirtSecrets(journey)
			_, err := journey.Service.Apply(context.Background(), apply)
			if interruption == "boot" || interruption == "release" {
				if err == nil {
					t.Fatal("an interrupted installation was recorded done")
				}
				holdKubevirtSecrets(journey)
				_, err = journey.Service.Apply(context.Background(), apply)
			}
			if err != nil {
				t.Fatalf("apply: %v (calls %v)", diagnostics.Of(err), host.calls)
			}
			if interruption != "eject" && host.failAt != "" {
				t.Fatalf("the %s interruption never fired", host.failAt)
			}
			if len(host.pinRefused) != 0 {
				t.Fatalf("nodes reached without their own pin: %v", host.pinRefused)
			}
			first := slices.Index(host.calls, agentinstall.InstallImplementation+" apply")
			for _, prior := range []string{kubevirt.HostImplementation + " apply", kubevirt.MachineImplementation + " apply", agentinstall.MediaImplementation + " apply"} {
				if position := slices.Index(host.calls, prior); position < 0 || position > first {
					t.Fatalf("%s did not precede the installation: %v", prior, host.calls)
				}
			}
			for _, name := range kubevirtNodes {
				if host.inserts[name] != 1 {
					t.Fatalf("%s was pushed %d times: %v", name, host.inserts[name], host.calls)
				}
				if host.uploaded[name] || !host.claims[name] || !host.running[name] {
					t.Fatalf("%s after release: upload %t, claim %t, running %t", name, host.uploaded[name], host.claims[name], host.running[name])
				}
			}
			holdKubevirtSecrets(journey)
			if _, err := journey.Service.Destroy(context.Background(), lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, Authorizations: []string{reconciliation.AuthorizationDataLoss}, SkipConfirmation: true}); err == nil {
				t.Fatal("the destroy removed running VirtualMachines")
			}
			for _, name := range kubevirtNodes {
				host.mutex.Lock()
				host.running[name], host.claims[name] = false, false
				host.mutex.Unlock()
			}
			destroy := lifecycle.DestroyRequest{ContextName: lifecycle.JourneyContext, Authorizations: []string{reconciliation.AuthorizationDataLoss}, SkipConfirmation: true}
			holdKubevirtSecrets(journey)
			_, err = journey.Service.Destroy(context.Background(), destroy)
			if interruption == "eject" {
				if err == nil || host.failAt != "" || len(host.machines) != len(kubevirtNodes) {
					t.Fatalf("an interrupted release removed the Machines or was recorded done: %v, %v", err, host.machines)
				}
				holdKubevirtSecrets(journey)
				_, err = journey.Service.Destroy(context.Background(), destroy)
			}
			if err != nil {
				t.Fatalf("destroy: %v (calls %v)", diagnostics.Of(err), host.calls)
			}
			if len(host.machines) != 0 || len(host.uploaded) != 0 || len(host.claims) != 0 {
				t.Fatalf("left behind: machines %v, uploads %v, claims %v", host.machines, host.uploaded, host.claims)
			}
			removal := strings.Join(host.calls[slices.Index(host.calls, agentinstall.InstallImplementation+" destroy"):], "\n")
			if !strings.Contains(removal, kubevirt.MachineImplementation+" destroy") {
				t.Fatalf("the Machines were not removed after the installation's release: %v", host.calls)
			}
		})
	}
}
