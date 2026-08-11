package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/render"
	stategraph "github.com/crmarques/bootwright/internal/state/graph"
)

var infraComponentSharedServiceSegmentRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func infraComponentSharedServiceSelectionDigest(state v1alpha1.State, service stategraph.MachineService) string {
	selection, ok := render.InfraComponentServiceSelection(state, service)
	if !ok {
		return ""
	}
	return infraComponentSharedServiceSelectionDigestForRendered(service.Identity.Kind, service.MachineRef, selection)
}

func infraComponentSharedServiceSelectionDigestForRendered(kind, host string, selection map[string]any) string {
	document := map[string]any{
		"apiVersion": "bootwright.io/infra-component-service-selection/v1alpha1",
		"kind":       strings.TrimSpace(kind),
		"host":       strings.TrimSpace(host),
		"component":  selection,
	}
	if document["kind"] == "" || document["host"] == "" || len(selection) == 0 {
		return ""
	}
	payload, err := infraComponentCanonicalJSON(document)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func infraComponentSharedServiceRecordDigests(record ownership.ResourceRecord) (string, string, error) {
	if record.APIVersion != "bootwright.io/ownership/v1alpha1" || record.Kind != string(ownership.KindInfraComponent) || record.Owner != ownership.Owner || record.EffectiveRole() != ownership.RoleOwner {
		return "", "", fmt.Errorf("record is not one exact Bootwright-owned infra-component ownership document")
	}
	for label, value := range map[string]string{
		"name": record.Name, "context": record.Context, "host": record.Host, "provider": record.Provider,
	} {
		if !infraComponentSharedServiceSegmentRE.MatchString(value) {
			return "", "", fmt.Errorf("record %s %q is not an exact safe identity", label, value)
		}
	}
	labels := record.Labels
	if len(labels) != 3 || labels["bootwright.provider"] != record.Provider || labels["bootwright.name"] == "" || !infraComponentSharedServiceSegmentRE.MatchString(labels["bootwright.name"]) || record.Name != record.Provider+"-"+labels["bootwright.name"] {
		return "", "", fmt.Errorf("record labels do not bind the exact provider/component identity")
	}
	kind := labels["bootwright.kind"]
	if record.Attributes["componentKind"] != kind {
		return "", "", fmt.Errorf("record component kind contradicts its labels")
	}
	attributes := make(map[string]string, len(record.Attributes))
	for key, value := range record.Attributes {
		if key != "destroyPhase" {
			attributes[key] = value
		}
	}
	if phase := record.Attributes["destroyPhase"]; phase != "" && phase != "external-cleanup-complete" {
		return "", "", fmt.Errorf("record destroy phase is not exact supported recovery evidence")
	}
	if err := validateInfraComponentSharedServiceRecordShape(kind, record.Provider, labels["bootwright.name"], record.Paths, attributes); err != nil {
		return "", "", err
	}
	side := map[string]any{
		"kind":       kind,
		"provider":   record.Provider,
		"name":       labels["bootwright.name"],
		"paths":      record.Paths,
		"labels":     labels,
		"attributes": attributes,
	}
	payload, err := infraComponentCanonicalJSON(side)
	if err != nil {
		return "", "", fmt.Errorf("encode infra-component record consequence: %w", err)
	}
	sum := sha256.Sum256(payload)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	return digest, digest, nil
}

func infraComponentTransitionSelectionDigest(record ownership.ResourceRecord) (string, error) {
	if record.APIVersion != "bootwright.io/ownership/v1alpha1" || record.Kind != string(ownership.KindInfraComponentTransition) || record.Owner != ownership.Owner || record.EffectiveRole() != ownership.RoleOwner {
		return "", fmt.Errorf("record is not one exact Bootwright-owned infra-component transition document")
	}
	for label, value := range map[string]string{
		"name": record.Name, "context": record.Context, "host": record.Host, "provider": record.Provider,
	} {
		if !infraComponentSharedServiceSegmentRE.MatchString(value) {
			return "", fmt.Errorf("transition record %s %q is not an exact safe identity", label, value)
		}
	}
	labels := record.Labels
	kind := labels["bootwright.kind"]
	componentName := labels["bootwright.name"]
	if len(labels) != 3 || labels["bootwright.provider"] != record.Provider || !infraComponentSharedServiceSegmentRE.MatchString(componentName) || record.Name != record.Provider+"-"+componentName || !supportedInfraComponentHostKind(kind) {
		return "", fmt.Errorf("transition record labels do not bind one exact supported provider/component identity")
	}
	claimPath := record.Attributes["claimPath"]
	if len(record.Attributes) != 2 || record.Attributes["componentKind"] != kind || !infraComponentAbsolutePath(claimPath) || !strings.HasSuffix(claimPath, "/transitions/infra-component/"+record.Name+".json") || !slices.Equal(record.Paths, []string{claimPath}) {
		return "", fmt.Errorf("transition record does not bind one exact canonical recovery claim")
	}
	document := map[string]any{
		"apiVersion": "bootwright.io/infra-component-transition-selection/v1alpha1",
		"kind":       kind,
		"name":       record.Name,
		"context":    record.Context,
		"host":       record.Host,
		"provider":   record.Provider,
		"paths":      record.Paths,
		"labels":     labels,
		"attributes": record.Attributes,
	}
	payload, err := infraComponentCanonicalJSON(document)
	if err != nil {
		return "", fmt.Errorf("encode infra-component transition selection: %w", err)
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func infraComponentCanonicalJSON(value any) ([]byte, error) {
	var encoded bytes.Buffer
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(encoded.Bytes(), []byte("\n")), nil
}

func supportedInfraComponentHostKind(kind string) bool {
	switch kind {
	case "artifacts", "load-balancer", "nameResolution", "ntp", "proxy", "registry":
		return true
	default:
		return false
	}
}

func validateInfraComponentSharedServiceRecordShape(kind, provider, name string, paths []string, attributes map[string]string) error {
	if len(paths) == 0 || !infraComponentAbsolutePath(paths[0]) || path.Base(paths[0]) != name {
		return fmt.Errorf("record state root is not one exact canonical component path")
	}
	stateRoot := paths[0]
	wantAttributes := func(keys ...string) bool {
		actual := make([]string, 0, len(attributes))
		for key := range attributes {
			actual = append(actual, key)
		}
		slices.Sort(actual)
		slices.Sort(keys)
		return slices.Equal(actual, keys)
	}
	switch kind {
	case "artifacts":
		if !wantAttributes("componentKind", "container", "listenerPorts") || attributes["container"] != "bootwright-artifacts-"+provider+"-"+name || !canonicalInfraComponentPorts(attributes["listenerPorts"], "tcp", true) || !slices.Equal(paths, []string{stateRoot}) {
			return fmt.Errorf("record artifact-server composite is not exact")
		}
	case "load-balancer":
		if !wantAttributes("componentKind", "container", "frontendPorts") || attributes["container"] != "bootwright-haproxy-"+provider+"-"+name || !canonicalInfraComponentPorts(attributes["frontendPorts"], "tcp", true) || !slices.Equal(paths, []string{stateRoot, "/etc/sysctl.d/99-bootwright-haproxy.conf"}) {
			return fmt.Errorf("record HAProxy composite is not exact")
		}
	case "nameResolution":
		if !wantAttributes("componentKind", "container", "port", "udpPort") || attributes["container"] != "bootwright-dnsmasq-"+provider+"-"+name || attributes["port"] != "53/tcp" || attributes["udpPort"] != "53/udp" || !slices.Equal(paths, []string{stateRoot}) {
			return fmt.Errorf("record dnsmasq composite is not exact")
		}
	case "ntp":
		includeDir := map[string]string{"chrony": "/etc/chrony/conf.d", "chronyd": "/etc/chrony.d"}[attributes["service"]]
		configPath := includeDir + "/bootwright-" + provider + "-" + name + ".conf"
		if includeDir == "" || !wantAttributes("componentKind", "port", "service") || !canonicalInfraComponentPorts(attributes["port"], "udp", false) || !slices.Equal(paths, []string{stateRoot, configPath}) {
			return fmt.Errorf("record NTP composite is not exact")
		}
	case "proxy":
		if !wantAttributes("componentKind", "container", "port") || attributes["container"] != "bootwright-squid-"+provider+"-"+name || !canonicalInfraComponentPorts(attributes["port"], "tcp", false) || !slices.Equal(paths, []string{stateRoot}) {
			return fmt.Errorf("record Squid composite is not exact")
		}
	case "registry":
		anchor := attributes["trustAnchor"]
		checksum := attributes["trustBundleSHA256"]
		wantAnchor := "/etc/pki/ca-trust/source/anchors/bootwright-mirror-" + provider + "-" + name + ".crt"
		if !wantAttributes("componentKind", "container", "port", "trustAnchor", "trustBundleSHA256") || attributes["container"] != "bootwright-mirror-registry-"+provider+"-"+name || !canonicalInfraComponentPorts(attributes["port"], "tcp", false) || ((anchor != "" || checksum != "") && (anchor != wantAnchor || regexp.MustCompile(`^[a-f0-9]{64}$`).FindString(checksum) != checksum)) || !slices.Equal(paths, []string{stateRoot}) {
			return fmt.Errorf("record mirror-registry composite is not exact")
		}
	default:
		return fmt.Errorf("record infra-component kind %q is not supported", kind)
	}
	return nil
}

func canonicalInfraComponentPorts(value, protocol string, multiple bool) bool {
	parts := strings.Split(value, ",")
	if (!multiple && len(parts) != 1) || len(parts) == 0 {
		return false
	}
	previous := 0
	for _, item := range parts {
		number, suffix, ok := strings.Cut(item, "/")
		portNumber, err := strconv.Atoi(number)
		if !ok || err != nil || strconv.Itoa(portNumber) != number || portNumber < 1 || portNumber > 65535 || suffix != protocol || portNumber <= previous {
			return false
		}
		previous = portNumber
	}
	return true
}

func infraComponentAbsolutePath(value string) bool {
	return strings.HasPrefix(value, "/") && value != "/" && path.Clean(value) == value
}
