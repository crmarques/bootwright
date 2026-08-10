package inventory

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/roles"
)

const controllerResolverOwnershipAPIVersion = "bootwright.io/ownership/v1alpha1"

const controllerResolverKind = "systemd-resolved"

type controllerNameResolutionDestroyTarget struct {
	Kind                         string
	ProviderName                 string
	Name                         string
	MachineRef                   string
	Realisation                  string
	DestroyRole                  string
	InfraComponentRecordName     string
	ControllerOwnershipName      string
	ControllerOwnershipPath      string
	ControllerResolverDropinPath string
	RecordBacked                 bool
	invalidReasons               []string
}

func controllerNameResolutionDestroyTargetsVars(state v1alpha1.State, records []ownership.ResourceRecord) []any {
	targets := map[string]controllerNameResolutionDestroyTarget{}
	for index, raw := range controllerNameResolutionServicesVars(state) {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		target := desiredControllerNameResolutionDestroyTarget(entry)
		key := controllerNameResolutionDestroyTargetKey(target, fmt.Sprintf("desired:%06d", index))
		if current, found := targets[key]; found {
			targets[key] = mergeControllerNameResolutionDestroyTargets(current, target)
			continue
		}
		targets[key] = target
	}
	for index, record := range records {
		if strings.TrimSpace(record.Kind) != string(ownership.KindControllerNameResolver) {
			continue
		}
		target := recordedControllerNameResolutionDestroyTarget(record)
		key := controllerNameResolutionDestroyTargetKey(target, fmt.Sprintf("record:%06d:%s", index, strings.TrimSpace(record.Name)))
		if current, found := targets[key]; found {
			targets[key] = mergeControllerNameResolutionDestroyTargets(current, target)
			continue
		}
		targets[key] = target
	}
	ordered := make([]controllerNameResolutionDestroyTarget, 0, len(targets))
	for _, target := range targets {
		ordered = append(ordered, target)
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].ProviderName != ordered[j].ProviderName {
			return ordered[i].ProviderName < ordered[j].ProviderName
		}
		if ordered[i].Name != ordered[j].Name {
			return ordered[i].Name < ordered[j].Name
		}
		return ordered[i].ControllerOwnershipName < ordered[j].ControllerOwnershipName
	})
	out := make([]any, 0, len(ordered))
	for _, target := range ordered {
		out = append(out, controllerNameResolutionDestroyTargetVars(target))
	}
	return out
}

func desiredControllerNameResolutionDestroyTarget(entry map[string]any) controllerNameResolutionDestroyTarget {
	target := controllerNameResolutionDestroyTarget{
		Kind:         stringMapValue(entry, "kind"),
		ProviderName: stringMapValue(entry, "providerName"),
		Name:         stringMapValue(entry, "name"),
		MachineRef:   stringMapValue(entry, "machineRef"),
		Realisation:  stringMapValue(entry, "realisation"),
	}
	target.InfraComponentRecordName = controllerInfraComponentRecordName(target.ProviderName, target.Name)
	target.DestroyRole, target.invalidReasons = controllerNameResolutionDestroyRole(target)
	return target
}

func recordedControllerNameResolutionDestroyTarget(record ownership.ResourceRecord) controllerNameResolutionDestroyTarget {
	target := controllerNameResolutionDestroyTarget{
		Kind:         v1alpha1.ComponentSlotNameResolution,
		ProviderName: strings.TrimSpace(record.Provider),
		Name:         strings.TrimSpace(record.Attributes["component"]),
		MachineRef:   strings.TrimSpace(record.Attributes["machineRef"]),
		Realisation:  strings.TrimSpace(record.Attributes["realisation"]),
		RecordBacked: true,
	}
	target.InfraComponentRecordName = controllerInfraComponentRecordName(target.ProviderName, target.Name)
	target.ControllerOwnershipName = controllerResolverOwnershipName(record.Context, target.ProviderName, target.Name)
	target.ControllerOwnershipPath = path.Join(ownership.ResourceDirName, string(ownership.KindControllerNameResolver), target.ControllerOwnershipName+".json")
	target.ControllerResolverDropinPath = controllerResolverDropinPath(target.ControllerOwnershipName)
	target.invalidReasons = controllerResolverRecordValidationErrors(record, target)
	role, roleErrors := controllerNameResolutionDestroyRole(target)
	target.DestroyRole = role
	target.invalidReasons = append(target.invalidReasons, roleErrors...)
	return target
}

func controllerNameResolutionDestroyRole(target controllerNameResolutionDestroyTarget) (string, []string) {
	var problems []string
	if target.Kind != v1alpha1.ComponentSlotNameResolution {
		problems = append(problems, fmt.Sprintf("kind %q must be %q", target.Kind, v1alpha1.ComponentSlotNameResolution))
	}
	if target.ProviderName != v1alpha1.KindInfraComponent {
		problems = append(problems, fmt.Sprintf("providerName %q must be %q", target.ProviderName, v1alpha1.KindInfraComponent))
	}
	for name, value := range map[string]string{
		"name":        target.Name,
		"machineRef":  target.MachineRef,
		"realisation": target.Realisation,
	} {
		if strings.TrimSpace(value) == "" {
			problems = append(problems, name+" is required")
		}
	}
	driver := roles.LookupService(target.Kind, target.Realisation)
	if driver.Status != roles.StatusSupported || strings.TrimSpace(driver.DestroyRole) == "" {
		problems = append(problems, fmt.Sprintf("service adapter %q/%q has no supported destroy role", target.Kind, target.Realisation))
		return "", problems
	}
	return driver.DestroyRole, problems
}

func controllerResolverRecordValidationErrors(record ownership.ResourceRecord, target controllerNameResolutionDestroyTarget) []string {
	var problems []string
	if err := ownership.ValidateResource(record); err != nil {
		problems = append(problems, err.Error())
	}
	if strings.TrimSpace(record.APIVersion) != controllerResolverOwnershipAPIVersion {
		problems = append(problems, fmt.Sprintf("apiVersion %q must be %q", record.APIVersion, controllerResolverOwnershipAPIVersion))
	}
	if strings.TrimSpace(record.Owner) != ownership.Owner {
		problems = append(problems, fmt.Sprintf("owner %q must be %q", record.Owner, ownership.Owner))
	}
	if record.EffectiveRole() != ownership.RoleOwner {
		problems = append(problems, fmt.Sprintf("role %q must be %q", record.EffectiveRole(), ownership.RoleOwner))
	}
	if strings.TrimSpace(record.Context) == "" {
		problems = append(problems, "context is required")
	}
	if strings.TrimSpace(record.Host) != "localhost" {
		problems = append(problems, fmt.Sprintf("host %q must be %q", record.Host, "localhost"))
	}
	if strings.TrimSpace(record.Attributes["resolver"]) != controllerResolverKind {
		problems = append(problems, fmt.Sprintf("resolver %q must be %q", record.Attributes["resolver"], controllerResolverKind))
	}
	if strings.TrimSpace(record.Labels["bootwright.kind"]) != v1alpha1.ComponentSlotNameResolution {
		problems = append(problems, fmt.Sprintf("label bootwright.kind %q must be %q", record.Labels["bootwright.kind"], v1alpha1.ComponentSlotNameResolution))
	}
	if strings.TrimSpace(record.Labels["bootwright.name"]) != target.Name {
		problems = append(problems, fmt.Sprintf("label bootwright.name %q must match component %q", record.Labels["bootwright.name"], target.Name))
	}
	if strings.TrimSpace(record.Name) != target.ControllerOwnershipName {
		problems = append(problems, fmt.Sprintf("ownership name %q must be derived as %q", record.Name, target.ControllerOwnershipName))
	}
	if len(record.Paths) != 1 || strings.TrimSpace(record.Paths[0]) != target.ControllerResolverDropinPath {
		problems = append(problems, fmt.Sprintf("paths %q must contain only %q", record.Paths, target.ControllerResolverDropinPath))
	}
	return problems
}

func mergeControllerNameResolutionDestroyTargets(current, candidate controllerNameResolutionDestroyTarget) controllerNameResolutionDestroyTarget {
	current.RecordBacked = current.RecordBacked || candidate.RecordBacked
	current.invalidReasons = append(current.invalidReasons, candidate.invalidReasons...)
	for name, pair := range map[string][2]string{
		"kind":         {current.Kind, candidate.Kind},
		"providerName": {current.ProviderName, candidate.ProviderName},
		"name":         {current.Name, candidate.Name},
		"machineRef":   {current.MachineRef, candidate.MachineRef},
		"realisation":  {current.Realisation, candidate.Realisation},
		"destroyRole":  {current.DestroyRole, candidate.DestroyRole},
	} {
		if pair[0] != pair[1] {
			current.invalidReasons = append(current.invalidReasons, fmt.Sprintf("%s conflicts between desired state %q and ownership evidence %q", name, pair[0], pair[1]))
		}
	}
	if current.ControllerOwnershipName == "" {
		current.ControllerOwnershipName = candidate.ControllerOwnershipName
		current.ControllerOwnershipPath = candidate.ControllerOwnershipPath
		current.ControllerResolverDropinPath = candidate.ControllerResolverDropinPath
	} else if candidate.ControllerOwnershipName != "" && current.ControllerOwnershipName != candidate.ControllerOwnershipName {
		current.invalidReasons = append(current.invalidReasons, fmt.Sprintf("ownership identity conflicts between %q and %q", current.ControllerOwnershipName, candidate.ControllerOwnershipName))
	}
	return current
}

func controllerNameResolutionDestroyTargetKey(target controllerNameResolutionDestroyTarget, fallback string) string {
	if target.ProviderName == "" || target.Name == "" {
		return fallback
	}
	return target.ProviderName + "\x00" + target.Name
}

func controllerNameResolutionDestroyTargetVars(target controllerNameResolutionDestroyTarget) map[string]any {
	problems := sortedUniqueStrings(target.invalidReasons)
	valid := len(problems) == 0
	destroyRole := target.DestroyRole
	if !valid {
		destroyRole = ""
	}
	out := map[string]any{
		"kind":                     target.Kind,
		"providerName":             target.ProviderName,
		"name":                     target.Name,
		"machineRef":               target.MachineRef,
		"realisation":              target.Realisation,
		"destroyRole":              destroyRole,
		"infraComponentRecordName": target.InfraComponentRecordName,
		"recordBacked":             target.RecordBacked,
		"valid":                    valid,
	}
	if target.ControllerOwnershipName != "" {
		out["controllerOwnershipName"] = target.ControllerOwnershipName
		out["controllerOwnershipPath"] = target.ControllerOwnershipPath
		out["controllerResolverDropinPath"] = target.ControllerResolverDropinPath
	}
	if !valid {
		out["validationError"] = strings.Join(problems, "; ")
	}
	return out
}

func controllerInfraComponentRecordName(provider, name string) string {
	provider = strings.TrimSpace(provider)
	name = strings.TrimSpace(name)
	if provider == "" || name == "" {
		return ""
	}
	return provider + "-" + name
}

func controllerResolverOwnershipName(contextName, provider, name string) string {
	values := []string{strings.TrimSpace(contextName), strings.TrimSpace(provider), strings.TrimSpace(name)}
	encoded := make([]string, 0, len(values))
	for _, value := range values {
		data, _ := json.Marshal(value)
		encoded = append(encoded, string(data))
	}
	digest := sha256.Sum256([]byte("[" + strings.Join(encoded, ", ") + "]"))
	return fmt.Sprintf("resolver-%x", digest)
}

func controllerResolverDropinPath(ownershipName string) string {
	return path.Join("/etc/systemd/resolved.conf.d", "bootwright-"+ownershipName+".conf")
}

func sortedUniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
