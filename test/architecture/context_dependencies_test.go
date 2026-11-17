package architecture_test

import (
	"path"
	"slices"
	"strconv"
	"strings"
	"testing"
)

type contextEdge struct {
	from, to, reason string
}

const questionable = "QUESTIONABLE: "

const contextsSpec = "the bounded-context table in specs/architecture.md"

// contextDependencies is the complete set of edges between bounded contexts,
// each with why the first context depends on the second. A context is the first
// path element under internal/ of a domain, application or adapter package
// (specs/architecture.md, Go package structure); the driving CLI, the
// composition root, the embedded assets and the technical packages are
// outside it. A new edge is a new coupling and needs its row and reason here.
// A reason that starts with QUESTIONABLE: marks an edge the specs do not
// explain; it stays until its owner decides.
func contextDependencies() []contextEdge {
	return []contextEdge{
		{"acquisition", "addons", "the list names the catalog images the selected add-on bindings and the default catalogs pull"},
		{"acquisition", "containercluster", "the list names a cluster's release repositories, mirrors and managed release copies through the installer's published selection"},
		{"acquisition", "controller", "the list names the controller's host packages and client downloads through the controller's published selection and client request"},
		{"acquisition", "infrastructureservices", "the list names each managed service's image and the managed mirrors' fill sets through Infrastructure services' published selection"},
		{"acquisition", "storage", "the list names a managed Ceph cluster's images and package repository through storage's published defaults"},
		{"acquisition", "substrate", "the list names the emulated BMC image a libvirt provider's Machines pull through the arm's published pulls"},
		{"addons", "controller", "cluster-object runs consume the controller evidence and host egress the controller publishes (applicationDependencies)"},
		{"addons", "desiredstate", "admission composes the compiled catalog and the custom-playbook records (Go package structure, Context root)"},
		{"addons", "machine", "applying objects to a cluster reaches the Machines that host it, through Machine's published target"},
		{"addons", "reconciliation", "instances and drivers implement the lifecycle capability port (Lifecycle ports)"},
		{"addons", "secrets", "package inputs, placements and secret references resolve through Secrets' custody and values"},
		{"containercluster", "addons", "secondary networks share the cluster-object programs and driver add-ons own (applicationDependencies)"},
		{"containercluster", "controller", "installer tools name the controller evidence (controller/evidence) and resolve a provider arm's client release through the tool table (controller/tooltable.ProviderTool)"},
		{"containercluster", "desiredstate", "capabilities read the compiled catalog they were planned from"},
		{"containercluster", "environment", "reads Environment's shared defaults and graph facts, such as the container-cluster domain and member selection"},
		{"containercluster", "infrastructureservices", "nodes select shared service endpoints through Infrastructure services' placement and endpoint rules"},
		{"containercluster", "machine", "cluster nodes bind Machines and read their addresses, network configuration and disks"},
		{"containercluster", "network", "node addresses and overlays are checked against Network's membership facts (Network row of " + contextsSpec + ")"},
		{"containercluster", "reconciliation", "capabilities implement the lifecycle capability port (Lifecycle ports)"},
		{"containercluster", "secrets", "cluster access and install material resolve through Secrets' custody and references"},
		{"containercluster", "substrate", "installation reads substrate facts and the capacity projection of the guests it needs"},
		{"controller", "addons", "tool selection reads which selected add-ons provide kubevirt through Add-ons' published binding expansion, provides and package instances (Context prerequisites, specs/controller.md)"},
		{"controller", "containercluster", "context prerequisite selection reads the cluster kind's published facts (containercluster.FactsOf; Context prerequisites, specs/controller.md)"},
		{"controller", "desiredstate", "the controller stage reads the compiled catalog and its selection values"},
		{"controller", "infrastructureservices", "selection reads the published mirror, service and Proxy facts (ManagedMirrors, ImageMirrorHost, HostsManagedService, ProxyFactsOf) and proves the route in the proxy grammar Infrastructure services owns (Egress and local effects, specs/controller.md)"},
		{"controller", "machine", "selection reads the controller Machine's published facts (OSProvided, ReachedOnlyLocally, ProxyChoice) and whether a mirror's Machine names the controller host (NamesControllerHost; Egress and local effects, specs/controller.md)"},
		{"controller", "managedos", "context prerequisite selection reads where Managed OS builds installer media (managedos.InstallerMediaHosts; Context prerequisites, specs/controller.md)"},
		{"controller", "reconciliation", "the controller stage is a lifecycle capability and its prerequisites feed the lifecycle workspace"},
		{"controller", "secrets", "clients and secret references resolve through Secrets' custody"},
		{"controller", "substrate", "context prerequisite selection reads the providers' published facts (ProviderTools, HostMachine, BridgeProviders; Context prerequisites, specs/controller.md)"},
		{"controller", "trust", "the host adapter reads SSH key identity through Trust's vocabulary"},
		{"desiredstate", "addons", "the compiler composes the add-on kind's admission rules (Go package structure, Context root)"},
		{"desiredstate", "environment", "the compiler composes Environment's admission and selection rules (Go package structure, Context root)"},
		{"environment", "containercluster", "cluster preflight is the union of the container and storage checks (Go package structure, Application dependencies)"},
		{"environment", "controller", "whole-environment preflight adds the controller's checks to the cluster union"},
		{"environment", "infrastructureservices", "inspection and access resolve shared service endpoints through Infrastructure services' rules"},
		{"environment", "machine", "access and inspection read Machine targets and posture"},
		{"environment", "reconciliation", "preflight selects the orchestration stages it checks through the reconciliation stage vocabulary"},
		{"environment", "secrets", "access and inspection resolve credentials through Secrets"},
		{"environment", "storage", "cluster preflight is the union of the container and storage checks (Go package structure, Application dependencies)"},
		{"environment", "substrate", "preflight reports capacity through the capacity port's graph and projector types"},
		{"infrastructureservices", "containercluster", questionable + "managedservice/requests.go reads a cluster kind's facts (containercluster.FactsOf); the Infrastructure services row excludes consumer cluster policy"},
		{"infrastructureservices", "desiredstate", "capabilities read the compiled catalog they were planned from"},
		{"infrastructureservices", "machine", "a service is placed on a Machine and reads its target, addresses and access"},
		{"infrastructureservices", "network", "endpoint addresses are checked against Network's membership facts"},
		{"infrastructureservices", "reconciliation", "capabilities implement the lifecycle capability port (Lifecycle ports)"},
		{"infrastructureservices", "secrets", "serving certificates and credentials resolve through Secrets"},
		{"infrastructureservices", "substrate", "service placement reads the substrate facts of the host Machine"},
		{"machine", "desiredstate", "capabilities read the compiled catalog they were planned from"},
		{"machine", "infrastructureservices", questionable + "Machine placement and admission read service placement rules, while Infrastructure services also depends on Machine"},
		{"machine", "network", "addresses are checked against Network's membership facts (Network row of " + contextsSpec + ")"},
		{"machine", "reconciliation", "capabilities implement the lifecycle capability port (Lifecycle ports)"},
		{"machine", "secrets", "access credentials resolve through Secrets' custody"},
		{"machine", "substrate", "a Machine's target is the realized substrate placement (substrate.TargetFor)"},
		{"machine", "trust", "access and sessions use Trust's host identity and decisions (Trust row of " + contextsSpec + ")"},
		{"managedos", "desiredstate", "capabilities read the compiled catalog they were planned from"},
		{"managedos", "infrastructureservices", "installation publishes through ArtifactServer's selection and path rules (applicationDependencies)"},
		{"managedos", "machine", "installation targets a Machine and reads its identity, disks and addresses"},
		{"managedos", "reconciliation", "capabilities implement the lifecycle capability port (Lifecycle ports)"},
		{"managedos", "secrets", "install profiles and credentials resolve through Secrets"},
		{"managedos", "substrate", "installation projects its bytes into the capacity budget and reads the substrate target"},
		{"managedos", "trust", "installation proves the host key it delivered through Trust's vocabulary (dependency proof)"},
		{"nativeartifacts", "desiredstate", "rendering compiles the effective state it writes (applicationDependencies)"},
		{"reconciliation", "controller", "the engine coordinates the controller evidence an operation crosses (applicationDependencies)"},
		{"reconciliation", "desiredstate", "the engine plans from the compiled catalog (applicationDependencies)"},
		{"reconciliation", "infrastructureservices", "the engine combines, qualifies and checks the host reservations a plan claims through Infrastructure services' reservation model (Host reservations)"},
		{"reconciliation", "machine", "the engine reads Machine targets, the SSH host a placement reaches and the host key it pins"},
		{"reconciliation", "secrets", "the engine coordinates custody and the secret context a transaction lends (applicationDependencies)"},
		{"reconciliation", "trust", "the engine and runner carry host-key evidence in Trust's vocabulary"},
		{"reconciliation", "workspace", "the context guard implements a Workspace-owned interface (Application store row, Go package structure)"},
		{"secrets", "desiredstate", "custody and the secret store read the compiled catalog and desired-state values (applicationDependencies)"},
		{"storage", "controller", "storage consumes the frozen controller egress controller setup publishes (applicationDependencies)"},
		{"storage", "desiredstate", "capabilities read the compiled catalog they were planned from"},
		{"storage", "environment", "reads Environment's graph facts such as cluster membership"},
		{"storage", "infrastructureservices", "the DVD package endpoint derives from ArtifactServer's published selection and path rules (applicationDependencies)"},
		{"storage", "machine", "cluster nodes bind Machines and read their disks, addresses and access"},
		{"storage", "managedos", questionable + "Ceph services validate RHSM entitlement and media names through Managed OS, whose row excludes storage-product entitlement policy"},
		{"storage", "network", "node addresses and networks are checked against Network's membership facts"},
		{"storage", "reconciliation", "capabilities implement the lifecycle capability port (Lifecycle ports)"},
		{"storage", "secrets", "cluster keys and service secrets resolve through Secrets' custody"},
		{"storage", "substrate", "preflight and capacity read substrate facts and the capacity projection"},
		{"storage", "trust", "node access uses Trust's host identity decisions"},
		{"substrate", "desiredstate", "arms read the compiled catalog they were planned from"},
		{"substrate", "infrastructureservices", "the emulated BMC's image acquisition uses a managed service's egress (applicationDependencies)"},
		{"substrate", "machine", "realization reads Machine identity, disks and addresses"},
		{"substrate", "network", "networks realized by an arm are Network declarations (Network row of " + contextsSpec + ")"},
		{"substrate", "reconciliation", "arms implement the lifecycle capability port (Lifecycle ports)"},
		{"substrate", "secrets", "management credentials resolve through Secrets' custody"},
		{"trust", "desiredstate", "enrollment reads the compiled catalog it was planned from"},
		{"trust", "machine", "enrollment selects the Machines whose host identity it records"},
		{"workspace", "controller", "contextfs stores the controller bundles, clients and setup evidence behind the controller's prerequisites ports"},
		{"workspace", "infrastructureservices", "contextfs stores the host reservations in the controller record and refuses conflicts through Infrastructure services' rules (Host reservations)"},
		{"workspace", "desiredstate", "contexts store and publish the admitted input revisions the compiler reads"},
		{"workspace", "managedos", "contextfs implements Managed OS's media store port (Port test support row, Go package structure)"},
		{"workspace", "reconciliation", "contextfs supplies the operation store's area and the log reader's source (Application store row)"},
		{"workspace", "secrets", "contextfs supplies the secret store's area and per-context keyring (Application store row)"},
	}
}

func contextOf(owner string, role packageRole) (string, bool) {
	rest, internal := strings.CutPrefix(owner, "internal/")
	if !internal || role == technicalRole || role == cliRole || role == compositionRole || role == embeddedRole {
		return "", false
	}
	name, _, _ := strings.Cut(rest, "/")
	return name, true
}

func TestContextDependenciesNameEveryEdge(t *testing.T) {
	for _, violation := range contextEdgeViolations(productionSources(t), packageRoles(), contextDependencies()) {
		t.Error(violation)
	}
}

func contextEdgeViolations(sources []sourceFile, roles map[string]packageRole, rows []contextEdge) []string {
	var violations []string
	declared := map[string]bool{}
	for _, row := range rows {
		key := row.from + " -> " + row.to
		switch {
		case declared[key]:
			violations = append(violations, "contextDependencies lists "+key+" twice; keep one row")
		case strings.TrimPrefix(row.reason, questionable) == "" || row.from == row.to:
			violations = append(violations, "contextDependencies row "+key+" needs a reason and two different contexts")
		}
		declared[key] = true
	}
	used := map[string]bool{}
	for _, source := range sources {
		from, ok := contextOf(source.owner, roles[source.owner])
		if !ok {
			continue
		}
		for _, imported := range source.imports {
			providerPath, local := strings.CutPrefix(imported.path, "github.com/crmarques/bootwright/")
			to, provided := contextOf(providerPath, roles[providerPath])
			if !local || !provided || to == from {
				continue
			}
			key := from + " -> " + to
			used[key] = true
			if !declared[key] {
				violations = append(violations, source.path+" imports "+providerPath+": context "+key+" has no row in contextDependencies; add one with its reason or remove the import")
			}
		}
	}
	for _, row := range rows {
		if key := row.from + " -> " + row.to; !used[key] {
			violations = append(violations, "contextDependencies lists "+key+", which no package imports any more; remove the row")
		}
	}
	slices.Sort(violations)
	return slices.Compact(violations)
}

// TestContextEdgeRuleRefusesWhatItGuards plants an undeclared edge, a stale
// and a repeated row and a row without a reason beside the edges the rule
// ignores: inside one context, to a technical package, from the CLI and to a
// package with no context.
func TestContextEdgeRuleRefusesWhatItGuards(t *testing.T) {
	const module = "github.com/crmarques/bootwright/"
	roles := map[string]packageRole{
		"internal/alpha": domainRole, "internal/alpha/inner": applicationRole, "internal/beta": domainRole,
		"internal/gamma": adapterRole, "internal/tech": technicalRole, "internal/cli": cliRole, "api/v1alpha1": domainRole,
	}
	imports := map[string][]string{
		"internal/alpha":       {"internal/alpha/inner", "internal/tech", "api/v1alpha1"},
		"internal/alpha/inner": {"internal/beta", "internal/gamma"},
		"internal/beta":        {"internal/alpha"},
		"internal/gamma":       {"internal/beta"},
		"internal/cli":         {"internal/alpha"},
	}
	var sources []sourceFile
	for _, owner := range sortedKeys(imports) {
		content := "package " + path.Base(owner) + "\nimport (\n"
		for _, provider := range imports[owner] {
			content += "\t_ " + strconv.Quote(module+provider) + "\n"
		}
		sources = append(sources, compositionSource(t, owner, content+")\n"))
	}
	rows := []contextEdge{
		{"alpha", "beta", "needed"}, {"beta", "alpha", ""}, {"gamma", "beta", "needed"}, {"gamma", "beta", "again"},
		{"beta", "gamma", "stale"},
	}
	want := []string{
		"contextDependencies lists beta -> gamma, which no package imports any more; remove the row",
		"contextDependencies lists gamma -> beta twice; keep one row",
		"contextDependencies row beta -> alpha needs a reason and two different contexts",
		"internal/alpha/inner/fixture.go imports internal/gamma: context alpha -> gamma has no row in contextDependencies; add one with its reason or remove the import",
	}
	if got := contextEdgeViolations(sources, roles, rows); !slices.Equal(got, want) {
		t.Fatalf("violations:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
