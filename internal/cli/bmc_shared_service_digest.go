package cli

import (
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

var bmcSharedServiceSegmentRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func bmcSharedServiceSelectionDigest(state v1alpha1.State, service stategraph.MachineService) string {
	version := ""
	for _, pin := range render.ComponentPins(state) {
		if pin.Name == "sushy-tools" {
			version = strings.TrimSpace(pin.Version)
			break
		}
	}
	return bmcSharedServiceSelectionDigestForVersion(service, version)
}

func bmcSharedServiceSelectionDigestForVersion(service stategraph.MachineService, sushyToolsVersion string) string {
	selection := map[string]string{
		"apiVersion":        "bootwright.io/bmc-service-selection/v1alpha1",
		"kind":              "bmc-emulator",
		"provider":          strings.TrimSpace(service.Identity.ProviderName),
		"host":              strings.TrimSpace(service.MachineRef),
		"realisation":       strings.TrimSpace(service.Fields["realisation"]),
		"applyRole":         strings.TrimSpace(service.Fields["applyRole"]),
		"destroyRole":       strings.TrimSpace(service.Fields["destroyRole"]),
		"bmcRole":           strings.TrimSpace(service.Fields["bmcRole"]),
		"configKey":         strings.TrimSpace(service.Fields["configKey"]),
		"sushyToolsVersion": strings.TrimSpace(sushyToolsVersion),
	}
	for _, key := range []string{"provider", "host", "realisation", "applyRole", "destroyRole", "bmcRole", "configKey", "sushyToolsVersion"} {
		if selection[key] == "" {
			return ""
		}
	}
	payload, err := json.Marshal(selection)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func bmcSharedServiceRecordDigests(record ownership.ResourceRecord) (string, string, error) {
	if record.APIVersion != "bootwright.io/ownership/v1alpha1" || record.Kind != string(ownership.KindBMCEmulator) || record.Owner != ownership.Owner || record.EffectiveRole() != ownership.RoleOwner {
		return "", "", fmt.Errorf("record is not one exact Bootwright-owned BMC ownership document")
	}
	for label, value := range map[string]string{
		"name": record.Name, "context": record.Context, "host": record.Host, "provider": record.Provider,
	} {
		if !bmcSharedServiceSegmentRE.MatchString(value) {
			return "", "", fmt.Errorf("record %s %q is not an exact safe identity", label, value)
		}
	}
	if record.Provider != record.Name {
		return "", "", fmt.Errorf("record provider %q does not match BMC name %q", record.Provider, record.Name)
	}
	expectedAttributeKeys := []string{
		"authPath", "bindAddress", "claimPath", "firewallManaged", "libvirtURI", "pool",
		"redfishPort", "redfishUnit", "stateRoot", "vMediaPort", "vMediaRoot", "vMediaUnit",
	}
	attributeKeys := make([]string, 0, len(record.Attributes))
	for key := range record.Attributes {
		attributeKeys = append(attributeKeys, key)
	}
	slices.Sort(attributeKeys)
	if !slices.Equal(attributeKeys, expectedAttributeKeys) {
		return "", "", fmt.Errorf("record BMC attributes are not the exact supported composite")
	}
	attrs := record.Attributes
	stateRoot := attrs["stateRoot"]
	vMediaRoot := attrs["vMediaRoot"]
	claimPath := attrs["claimPath"]
	if !bmcAbsolutePath(stateRoot) || !strings.HasSuffix(stateRoot, "/bmc/"+record.Name) {
		return "", "", fmt.Errorf("record BMC state root %q is not canonical", stateRoot)
	}
	wantVMediaRoot := "/var/lib/libvirt/images/bootwright/" + record.Context + "/bmc/" + record.Name + "/vmedia"
	wantClaimPath := "/var/lib/bootwright/shared-services/bmc-emulator/" + record.Name
	wantRedfishUnit := "bootwright-sushy-" + record.Name + ".service"
	wantVMediaUnit := "bootwright-vmedia-" + record.Name + ".service"
	if vMediaRoot != wantVMediaRoot || claimPath != wantClaimPath || attrs["redfishUnit"] != wantRedfishUnit || attrs["vMediaUnit"] != wantVMediaUnit || attrs["pool"] != "bootwright-"+record.Name+"-vmedia" {
		return "", "", fmt.Errorf("record BMC pool, claim, vMedia, or unit identity is not canonical")
	}
	if attrs["libvirtURI"] == "" || attrs["bindAddress"] == "" || (attrs["authPath"] != "" && attrs["authPath"] != stateRoot+"/htpasswd") || (attrs["firewallManaged"] != "true" && attrs["firewallManaged"] != "false") {
		return "", "", fmt.Errorf("record BMC URI, bind, auth, or firewall identity is invalid")
	}
	redfishPort, redfishErr := strconv.Atoi(attrs["redfishPort"])
	vMediaPort, vMediaErr := strconv.Atoi(attrs["vMediaPort"])
	if redfishErr != nil || vMediaErr != nil || redfishPort < 1 || redfishPort > 65535 || vMediaPort < 1 || vMediaPort > 65535 || redfishPort == vMediaPort || strconv.Itoa(redfishPort) != attrs["redfishPort"] || strconv.Itoa(vMediaPort) != attrs["vMediaPort"] {
		return "", "", fmt.Errorf("record BMC ports are not two distinct canonical ports")
	}
	wantPaths := []string{
		stateRoot,
		vMediaRoot,
		"/etc/systemd/system/" + wantRedfishUnit,
		"/etc/systemd/system/" + wantVMediaUnit,
		claimPath,
	}
	if !slices.Equal(record.Paths, wantPaths) {
		return "", "", fmt.Errorf("record BMC paths do not match the exact composite")
	}
	consequenceAttributes := make(map[string]string, len(attrs)-1)
	for key, value := range attrs {
		if key != "firewallManaged" {
			consequenceAttributes[key] = value
		}
	}
	consequence := map[string]any{
		"apiVersion": "bootwright.io/bmc-host-consequence/v1alpha1",
		"kind":       "bmc-emulator",
		"name":       record.Name,
		"context":    record.Context,
		"host":       record.Host,
		"provider":   record.Provider,
		"paths":      record.Paths,
		"attributes": consequenceAttributes,
	}
	payload, err := json.Marshal(consequence)
	if err != nil {
		return "", "", fmt.Errorf("encode BMC record consequence: %w", err)
	}
	sum := sha256.Sum256(payload)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	return digest, digest, nil
}

func bmcAbsolutePath(value string) bool {
	return strings.HasPrefix(value, "/") && value != "/" && path.Clean(value) == value
}
