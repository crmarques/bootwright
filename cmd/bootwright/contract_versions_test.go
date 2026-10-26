//go:build linux && amd64

package main

import (
	"encoding"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/addons/clusterobjects/driver"
	"github.com/crmarques/bootwright/internal/addons/instance"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/containercluster/networking"
	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/controller/runtimelock"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/dnsserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/infrastructureservices/ntpserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/proxy"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/storage/ceph"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

// contractVersion is one implementation's contract: its content digest and
// the Go types its request and evidence encode from.
type contractVersion struct {
	implementation string
	digest         string
	request        reflect.Type
	evidence       reflect.Type
}

func contractVersions() []contractVersion {
	versions := []contractVersion{
		{instance.Implementation, instance.ContentDigest(), reflect.TypeFor[instance.Request](), reflect.TypeFor[instance.Evidence]()},
		{networking.Implementation, networking.ContentDigest(), reflect.TypeFor[networking.Request](), reflect.TypeFor[driver.Evidence]()},
		{clients.Implementation, clients.ContentDigest(), reflect.TypeFor[clients.Request](), reflect.TypeFor[clients.Evidence]()},
		{libvirt.HostImplementation, libvirt.HostContentDigest(), reflect.TypeFor[libvirt.HostRequest](), reflect.TypeFor[libvirt.HostEvidence]()},
		{libvirt.MachineImplementation, libvirt.MachineContentDigest(), reflect.TypeFor[libvirt.MachineRequest](), reflect.TypeFor[libvirt.MachineEvidence]()},
		{baremetal.Implementation, baremetal.ContentDigest(), reflect.TypeFor[baremetal.Request](), reflect.TypeFor[baremetal.Evidence]()},
		{installation.TreeImplementation, installation.TreeContentDigest(), reflect.TypeFor[installation.TreeRequest](), reflect.TypeFor[installation.TreeEvidence]()},
		{installation.EraseImplementation, installation.EraseContentDigest(), reflect.TypeFor[installation.EraseRequest](), reflect.TypeFor[installation.EraseEvidence]()},
		{installation.Implementation, installation.ContentDigest(), reflect.TypeFor[installation.Request](), reflect.TypeFor[installation.Evidence]()},
		{agentinstall.MediaImplementation, agentinstall.MediaContentDigest(), reflect.TypeFor[agentinstall.MediaRequest](), reflect.TypeFor[agentinstall.MediaEvidence]()},
		{agentinstall.InstallImplementation, agentinstall.InstallContentDigest(), reflect.TypeFor[agentinstall.InstallRequest](), reflect.TypeFor[agentinstall.InstallEvidence]()},
		{artifactserver.Implementation, artifactserver.ContentDigest(), reflect.TypeFor[artifactserver.Request](), reflect.TypeFor[artifactserver.Evidence]()},
		{ceph.Implementation, ceph.ContentDigest(), reflect.TypeFor[ceph.Request](), reflect.TypeFor[ceph.Evidence]()},
		{ceph.ImageImplementation, ceph.ImageContentDigest(), reflect.TypeFor[ceph.ImageRequest](), reflect.TypeFor[ceph.ImageEvidence]()},
		{ceph.HostImplementation, ceph.HostContentDigest(), reflect.TypeFor[ceph.HostRequest](), reflect.TypeFor[ceph.HostEvidence]()},
		{ceph.PhysicalRetirementImplementation, ceph.PhysicalRetirementContentDigest(), reflect.TypeFor[ceph.PhysicalRetirementRequest](), reflect.TypeFor[ceph.PhysicalRetirementEvidence]()},
		{ceph.SMBImplementation, ceph.SMBContentDigest(), reflect.TypeFor[ceph.SMBRequest](), reflect.TypeFor[ceph.SMBEvidence]()},
		{ceph.ReplicationImplementation, ceph.ReplicationContentDigest(), reflect.TypeFor[ceph.ReplicationRequest](), reflect.TypeFor[ceph.ReplicationEvidence]()},
		{ceph.ExportImplementation, ceph.ExportContentDigest(), reflect.TypeFor[ceph.ExportRequest](), reflect.TypeFor[ceph.ExportEvidence]()},
	}
	for _, definition := range []managedservice.Definition{proxy.Definition(), dnsserver.Definition(), ntpserver.Definition()} {
		versions = append(versions, contractVersion{definition.Implementation, definition.ContentDigest(),
			reflect.TypeFor[managedservice.Request](), reflect.TypeFor[managedservice.Evidence]()})
	}
	return versions
}

// Automation format classes: what a versioned literal in the embedded
// collection names. Only a persisted format belongs to an implementation's
// contract; the others are already a content-digest input or gated by the
// automation digest.
const (
	formatPersisted  = "persisted"
	formatMirrored   = "mirrored"
	formatTransient  = "transient"
	formatController = "controller"
)

// automationFormat classifies one literal; a persisted one names every
// implementation whose host data it versions.
type automationFormat struct {
	class           string
	implementations []string
}

func persistedBy(implementations ...string) automationFormat {
	return automationFormat{class: formatPersisted, implementations: implementations}
}

// automationFormats classifies every versioned literal the embedded plugins
// and roles carry, by where each is written and read.
func automationFormats() map[string]automationFormat {
	host, core := persistedBy(ceph.HostImplementation), persistedBy(ceph.Implementation)
	return map[string]automationFormat{
		"addon-instance-request-v1":           {class: formatMirrored},
		"ceph-export-request-v1":              {class: formatMirrored},
		"ceph-export-evidence-v1":             {class: formatMirrored},
		"cluster-network-request-v1":          {class: formatMirrored},
		"cluster-objects-v1":                  {class: formatMirrored},
		"bootwright.cluster-objects.owner-v1": {class: formatMirrored},
		// The host block writes them; the core block's workers read them back
		// for the manager account, the cephadm cache and the OSD identities.
		"ceph-runtime-profile-v3":         persistedBy(ceph.HostImplementation, ceph.Implementation),
		"ceph-physical-device-binding-v1": persistedBy(ceph.HostImplementation, ceph.Implementation),
		"ceph-device-identities-v2":       persistedBy(ceph.HostImplementation, ceph.Implementation),
		"ceph-host-route-v3":              host,
		"ceph-package-decision-v1":        host,
		"ceph-package-artifacts-v1":       host,
		"ceph-package-complete-v1":        host,
		"ceph-crypto-policy-v1":           host,
		// The resolved package plan the host's package decision record holds.
		"bootwright.native-plan-v1": host,

		"ceph-tunnel-v3":                    core,
		"ceph-tunnel-custody-v1":            core,
		"ceph-replication-custody-v1":       persistedBy(ceph.ReplicationImplementation),
		"ceph-tunnel-relabel-v1":            core,
		"ceph-tunnel-stage-v1":              core,
		"ceph-tunnel-redeploy-v1":           core,
		"ceph-bootstrap-recovery-v1":        core,
		"ceph-bootstrap-state-v1":           core,
		"ceph-registry-v1":                  core,
		"ceph-native-teardown-v1":           core,
		"ceph-pool-attempt-v2":              core,
		"ceph-policy-attempt-v1":            core,
		"ceph-rbd-image-attempt-v1":         core,
		"ceph-filesystem-attempt-v1":        core,
		"ceph-subvolume-group-attempt-v1":   core,
		"ceph-subvolume-attempt-v1":         core,
		"ceph-client-auth-v1":               core,
		"ceph-dashboard-redeploy-v1":        core,
		"ceph-dashboard-port-repair-v1":     core,
		"ceph-dashboard-port-attempt-v1":    core,
		"ceph-dashboard-user-v1":            core,
		"ceph-cluster-device-identities-v2": core,
		"ceph-osd-device-intent-v1":         core,
		"ceph-service-inputs-v1":            core,
		// The core block's cache phase publishes cephadm on each host under it.
		"ceph-host-cache-v1": core,
		// The client and dashboard credential custody records, by kind.
		"-custody-v1": core,

		"https://bootwright.io/substrate/v1": persistedBy(libvirt.HostImplementation, libvirt.MachineImplementation),

		"ceph-host-image-v1":                 {class: formatTransient},
		"ceph-host-device-id-v1":             {class: formatTransient},
		"bootwright-ceph-route-probe-v1":     {class: formatTransient},
		"bootwright-ceph-route-prepare-v1":   {class: formatTransient},
		"bootwright-ceph-route-synthetic-v1": {class: formatTransient},
		// The registry client's User-Agent.
		"bootwright-ceph-metadata-v1": {class: formatTransient},
		"bootwright-pull-broker-v1":   {class: formatTransient},
		// The bytes the label probe writes in its own scratch and reads back.
		"bootwright-contained-public-label-probe-v1": {class: formatTransient},
		// The guest package transaction policy ceph_packages.transaction_request builds and the shared resolver reads within one run.
		"ceph-packages-v2": {class: formatTransient},

		"ceph-host-v7":                 {class: formatMirrored},
		"ceph-host-evidence-v3":        {class: formatMirrored},
		"ceph-core-v32":                {class: formatMirrored},
		"ceph-core-evidence-v11":       {class: formatMirrored},
		"ceph-packages-v3":             {class: formatMirrored},
		"ceph-image-v4":                {class: formatMirrored},
		"ceph-smb-v1":                  {class: formatMirrored},
		"ceph-smb-evidence-v1":         {class: formatMirrored},
		"os-package-tree-evidence-v1":  {class: formatMirrored},
		"ceph-replication-v1":          {class: formatMirrored},
		"ceph-replication-evidence-v1": {class: formatMirrored},
		// The request versions each role refuses unless its request names.
		"substrate-host-libvirt-v6": {class: formatMirrored},
		"machine-libvirt-v8":        {class: formatMirrored},
		"machine-baremetal-v2":      {class: formatMirrored},
		"os-package-tree-v1":        {class: formatMirrored},
		"os-install-anaconda-v16":   {class: formatMirrored},
		"physical-erase-v1":         {class: formatMirrored},
		"cluster-media-agent-v9":    {class: formatMirrored},
		"cluster-install-agent-v7":  {class: formatMirrored},
		"artifact-server-nginx-v2":  {class: formatMirrored},
		"managed-service-v3":        {class: formatMirrored},
		// Power is no plan block: each run binds its request to the power
		// content digest these implementations feed, and nothing is retained.
		"machine-power-redfish-v2": {class: formatMirrored},
		"machine-power-read-v2":    {class: formatMirrored},

		"controller-prerequisites-v6": {class: formatController},
	}
}

// formatSelfHealing is a store format Go code writes and reads itself: another
// build's copy is discarded by setup after confirmation and refuses a launch
// naming setup, so it versions no implementation's contract (D152.9).
// formatRuntimeIdentity is a digest domain the setup receipt records: a moved
// one names another runtime, which setup prepares beside the old (D152.7).
const (
	formatSelfHealing     = "self-healing"
	formatRuntimeIdentity = "runtime-identity"
)

// storeFormats classifies the versioned formats the store persists outside
// any implementation's automation, by how another build's copy is met.
func storeFormats() map[string]string {
	return map[string]string{
		prerequisites.SetupStateFormat:     formatSelfHealing,
		runtimelock.Format:                 formatRuntimeIdentity,
		prerequisites.RuntimeFormat:        formatRuntimeIdentity,
		prerequisites.RuntimeClosureFormat: formatRuntimeIdentity,
	}
}

// TestEveryStoreFormatIsClassified holds each store format to a versioned
// name no automation literal also claims, so a moved setup format is a new,
// classified literal rather than a silent reuse.
func TestEveryStoreFormatIsClassified(t *testing.T) {
	versioned := regexp.MustCompile(`-v[0-9]+$`)
	automation := automationFormats()
	for literal, class := range storeFormats() {
		if !versioned.MatchString(literal) {
			t.Errorf("store format %q does not end in a version", literal)
		}
		if class != formatSelfHealing && class != formatRuntimeIdentity {
			t.Errorf("store format %q has class %q, want %q or %q", literal, class, formatSelfHealing, formatRuntimeIdentity)
		}
		if _, claimed := automation[literal]; claimed {
			t.Errorf("store format %q is also classified as an automation literal", literal)
		}
	}
	for literal, want := range map[string]string{
		prerequisites.SetupStateFormat:     "bootwright.setup-state-v1",
		runtimelock.Format:                 "bootwright.controller.runtime-lock-v1",
		prerequisites.RuntimeFormat:        "bootwright.controller.runtime-v1",
		prerequisites.RuntimeClosureFormat: "bootwright.controller.execution-closure-v2",
	} {
		if literal != want {
			t.Errorf("the store format %q moved to %q: classify the new literal", want, literal)
		}
	}
}

const automationCollection = "collections/ansible_collections/bootwright/core/"

var (
	automationTexts   = []string{".py", ".yml", ".yaml", ".j2"}
	versionedLiteral  = regexp.MustCompile(`"([^"\\\n]*-v[0-9]+)(?:\\[nrt0])?"|'([^'\\\n]*-v[0-9]+)(?:\\[nrt0])?'`)
	bootwrightLiteral = regexp.MustCompile(`["'](https://bootwright\.io/[^"'\\\s]*)["']`)
)

// automationLiterals is every quoted versioned literal the collection's
// embedded plugins and roles carry, with the files that carry it.
func automationLiterals() map[string][]string {
	found := map[string][]string{}
	assets := ansible.Assets()
	for _, name := range slices.Sorted(maps.Keys(assets)) {
		if !strings.HasPrefix(name, automationCollection) || !slices.Contains(automationTexts, filepath.Ext(name)) {
			continue
		}
		var literals []string
		for _, match := range versionedLiteral.FindAllStringSubmatch(string(assets[name]), -1) {
			literals = append(literals, match[1]+match[2])
		}
		for _, match := range bootwrightLiteral.FindAllStringSubmatch(string(assets[name]), -1) {
			literals = append(literals, match[1])
		}
		for _, literal := range literals {
			if !slices.Contains(found[literal], name) {
				found[literal] = append(found[literal], name)
			}
		}
	}
	return found
}

// TestEveryAutomationVersionLiteralIsClassified holds the classification of
// the quoted literals ending in a version, or naming a bootwright.io URI, that
// the collection's embedded plugins and roles carry (.py, .yml, .yaml and .j2)
// exact, so a new or renamed quoted format cannot reach a host without saying
// whether it is part of an implementation's contract. An unquoted, dynamically
// built or unversioned format is outside what it sees.
func TestEveryAutomationVersionLiteralIsClassified(t *testing.T) {
	found, formats := automationLiterals(), automationFormats()
	if len(found) == 0 {
		t.Fatal("the embedded collection carries no versioned literal: the scan is broken")
	}
	implementations := map[string]bool{}
	for _, version := range contractVersions() {
		implementations[version.implementation] = true
	}
	for _, literal := range slices.Sorted(maps.Keys(found)) {
		if _, ok := formats[literal]; !ok {
			t.Errorf("%q in %s is not classified: classify it in automationFormats", literal, strings.Join(found[literal], ", "))
		}
	}
	for _, literal := range slices.Sorted(maps.Keys(formats)) {
		format := formats[literal]
		if _, ok := found[literal]; !ok {
			t.Errorf("%q is classified but no embedded plugin or role carries it: stale classification, remove it", literal)
		}
		if (format.class == formatPersisted) != (len(format.implementations) != 0) {
			t.Errorf("%q: only a persisted format names implementations", literal)
		}
		for _, implementation := range format.implementations {
			if !implementations[implementation] {
				t.Errorf("%q names %s, which no contract version names", literal, implementation)
			}
		}
	}
}

// TestEveryImplementationMovesItsContentDigestWithItsContract holds each
// implementation's contract, its Go request and evidence schemas and the
// formats its automation persists on a host, to its content digest: the
// digest is what a continuation under another build compares, so a contract
// that changes under the same digest would let another build continue work
// half done under the old contract. Such a change fails and -update refuses
// it: move the implementation's version. A moved digest is rewritten by
// -update. A raw-JSON, interface or map[string]any member is fingerprinted as
// an opaque leaf, so the schema nested in one, such as the raw cluster
// services the Ceph core and host requests carry or the agent media's install
// and agent configurations, is the author's to move; re-planning still
// compares its bytes.
func TestEveryImplementationMovesItsContentDigestWithItsContract(t *testing.T) {
	versions := contractVersions()
	contractOfferedImplementations(t, versions)
	persisted, formats := map[string][]string{}, automationFormats()
	for literal := range automationLiterals() {
		for _, implementation := range formats[literal].implementations {
			persisted[implementation] = append(persisted[implementation], literal)
		}
	}
	directory := filepath.Join("testdata", "contracts")
	wanted := map[string]bool{}
	for _, version := range versions {
		name := version.implementation + ".golden"
		wanted[name] = true
		text := contractText(version, persisted[version.implementation])
		contractHoldGolden(t, version, filepath.Join(directory, name), text)
	}
	entries, err := os.ReadDir(directory)
	if err != nil && !*update {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !wanted[entry.Name()] {
			t.Errorf("%s names no implementation a contract version names: remove it", filepath.Join(directory, entry.Name()))
		}
	}
}

// contractOfferedImplementations proves the versions name exactly the
// implementations the composition root offers, and each its digest as the
// real capability plans it on the examples the capability contract plans.
func contractOfferedImplementations(t *testing.T, versions []contractVersion) {
	t.Helper()
	resolver := buildCapabilitiesWith(systemClock{}, exampleControllerPorts(t), &contractRunner{}, exampleMediaRecords{})
	offered, listed := map[string]bool{}, map[string]string{}
	for _, binding := range resolver.Bindings() {
		offered[binding.Implementation] = true
	}
	for _, version := range versions {
		if _, twice := listed[version.implementation]; twice {
			t.Errorf("%s is listed twice", version.implementation)
		}
		listed[version.implementation] = version.digest
		if !offered[version.implementation] {
			t.Errorf("%s is listed but the composition root offers no such implementation", version.implementation)
		}
	}
	for implementation := range offered {
		if _, ok := listed[implementation]; !ok {
			t.Errorf("%s is offered but has no contract version: add it to contractVersions", implementation)
		}
	}
	planned := map[string]bool{}
	examples := map[string]bool{}
	for _, row := range contractRows() {
		if examples[row.example] {
			continue
		}
		examples[row.example] = true
		for _, block := range contractExampleOf(t, resolver, row.example).plan.Blocks {
			planned[block.Implementation] = true
			if digest, ok := listed[block.Implementation]; ok && block.ContentDigest != digest {
				t.Errorf("%s plans %s under content digest %s, not the %s its contract version names", block.Implementation, block.ID, block.ContentDigest, digest)
			}
		}
	}
	for implementation := range listed {
		if !planned[implementation] {
			t.Errorf("no example plans %s, so its contract digest is not held to what it plans", implementation)
		}
	}
}

// contractSections is the order of a contract golden's sections.
var contractSections = []string{"implementation", "content-digest", "automation-formats", "request-schema", "evidence-schema"}

func contractText(version contractVersion, persisted []string) string {
	formats := "none"
	if len(persisted) != 0 {
		formats = strings.Join(slices.Sorted(slices.Values(persisted)), ", ")
	}
	var text strings.Builder
	fmt.Fprintf(&text, "implementation: %s\ncontent-digest: %s\nautomation-formats: %s\n", version.implementation, version.digest, formats)
	for _, schema := range []struct {
		name  string
		value reflect.Type
	}{{"request-schema", version.request}, {"evidence-schema", version.evidence}} {
		text.WriteString(schema.name + ":\n")
		lines := schemaFingerprint(schema.value)
		if len(lines) == 0 {
			lines = []string{"none"}
		}
		for _, line := range lines {
			text.WriteString("  " + line + "\n")
		}
	}
	return text.String()
}

// contractSectionsOf splits a contract golden into its sections by name.
func contractSectionsOf(text string) map[string]string {
	sections := map[string]string{}
	current := ""
	for line := range strings.SplitSeq(strings.TrimSuffix(text, "\n"), "\n") {
		if !strings.HasPrefix(line, "  ") {
			name, value, _ := strings.Cut(line, ":")
			current = name
			sections[current] = strings.TrimSpace(value)
			continue
		}
		sections[current] += "\n" + line
	}
	return sections
}

func contractHoldGolden(t *testing.T, version contractVersion, path, text string) {
	t.Helper()
	golden, err := os.ReadFile(path)
	if err != nil {
		if !*update {
			t.Errorf("%s: %v; run with -update to write it", version.implementation, err)
			return
		}
		contractWriteGolden(t, path, text)
		return
	}
	if string(golden) == text {
		return
	}
	recorded, current := contractSectionsOf(string(golden)), contractSectionsOf(text)
	if recorded["content-digest"] == current["content-digest"] {
		var changed []string
		for _, section := range contractSections {
			if recorded[section] != current[section] {
				changed = append(changed, section)
			}
		}
		t.Errorf("%s: its %s changed while its content digest did not, so another build would continue its blocks under the old contract: move %s's version, which moves its content digest (-update refuses to record this):\n%s",
			version.implementation, strings.Join(changed, " and "), version.implementation, lineDiff(string(golden), text))
		return
	}
	if !*update {
		t.Errorf("%s: its content digest moved; rerun with -update to record its contract:\n%s", version.implementation, lineDiff(string(golden), text))
		return
	}
	contractWriteGolden(t, path, text)
}

func contractWriteGolden(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

var (
	schemaRawMessage    = reflect.TypeFor[json.RawMessage]()
	schemaJSONMarshaler = reflect.TypeFor[json.Marshaler]()
	schemaTextMarshaler = reflect.TypeFor[encoding.TextMarshaler]()
)

// schemaFingerprint is one line per field encoding/json writes for a value
// of t, in declared order: its path, kind and tag options. A nested struct
// names its Go type, so a type swap shows; raw JSON, interfaces and
// map[string]any are opaque leaves whose nested schema it never sees.
func schemaFingerprint(t reflect.Type) []string {
	if t == nil {
		return nil
	}
	walker := schemaWalker{visiting: map[reflect.Type]bool{}}
	walker.fields("", t)
	return walker.lines
}

type schemaWalker struct {
	lines    []string
	visiting map[reflect.Type]bool
}

func (w *schemaWalker) fields(path string, t reflect.Type) {
	if w.visiting[t] {
		w.lines = append(w.lines, path+" cycle "+t.String())
		return
	}
	w.visiting[t] = true
	defer delete(w.visiting, t)
	for index := range t.NumField() {
		field := t.Field(index)
		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, options, _ := strings.Cut(tag, ",")
		inner := field.Type
		if inner.Kind() == reflect.Pointer {
			inner = inner.Elem()
		}
		if field.Anonymous && name == "" && inner.Kind() == reflect.Struct {
			w.fields(path, inner)
			continue
		}
		if !field.IsExported() {
			continue
		}
		if name == "" {
			name = field.Name
		}
		line := path + name + " " + w.kind(field.Type)
		if options != "" {
			line += " " + strings.ReplaceAll(options, ",", " ")
		}
		w.lines = append(w.lines, line)
		w.nested(path+name, field.Type)
	}
}

// kind names how encoding/json writes a value of t.
func (w *schemaWalker) kind(t reflect.Type) string {
	switch {
	case t == schemaRawMessage || t.Kind() == reflect.Interface:
		return "opaque"
	case t.Kind() == reflect.Map && t.Key().Kind() == reflect.String && t.Elem().Kind() == reflect.Interface:
		return "opaque"
	case t.Implements(schemaJSONMarshaler) || reflect.PointerTo(t).Implements(schemaJSONMarshaler):
		return "marshaler " + t.String()
	case t.Implements(schemaTextMarshaler) || reflect.PointerTo(t).Implements(schemaTextMarshaler):
		return "text " + t.String()
	}
	switch t.Kind() {
	case reflect.Pointer:
		return "*" + w.kind(t.Elem())
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 && t.Kind() == reflect.Slice {
			return "bytes"
		}
		return "[]" + w.kind(t.Elem())
	case reflect.Map:
		return "map[" + t.Key().Kind().String() + "]" + w.kind(t.Elem())
	case reflect.Struct:
		return "struct " + t.String()
	}
	return t.Kind().String()
}

// nested walks the fields of every struct a field of t holds, under the path
// its encoding nests them at.
func (w *schemaWalker) nested(path string, t reflect.Type) {
	if kind := w.kind(t); kind == "opaque" || strings.HasPrefix(kind, "marshaler ") || strings.HasPrefix(kind, "text ") {
		return
	}
	switch t.Kind() {
	case reflect.Pointer:
		w.nested(path, t.Elem())
	case reflect.Slice, reflect.Array:
		w.nested(path+"[]", t.Elem())
	case reflect.Map:
		w.nested(path+"{}", t.Elem())
	case reflect.Struct:
		w.fields(path+".", t)
	}
}
