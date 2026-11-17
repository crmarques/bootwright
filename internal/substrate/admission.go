package substrate

import (
	"fmt"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

var armNormalizers = map[Arm]func(arm api.Value) api.Value{
	ArmBaremetal: normalizeBaremetal,
	ArmLibvirt:   normalizeLibvirt,
	ArmVSphere:   normalizeVSphere,
}

var providerNormalizers = map[Arm]func(spec, arm api.Value, catalog api.Catalog) api.Value{
	ArmKubeVirt: normalizeKubeVirt,
}

var authoredValidators = map[Arm]func(o api.Object) []api.Issue{
	ArmBaremetal: validateBaremetalAuthored,
	ArmVSphere:   validateVSphereAuthored,
}

var armValidators = map[Arm]func(o api.Object, c api.Catalog) []api.Issue{
	ArmBaremetal: validateBaremetal,
	ArmLibvirt:   validateLibvirt,
	ArmVSphere:   validateVSphere,
}

var segmentValidators = map[Arm]func(path string, segment, arm api.Value) []api.Issue{
	ArmVSphere:  validateVSphereSegment,
	ArmKubeVirt: validateKubeVirtSegment,
}

func Normalize(o api.Object, catalog api.Catalog) (api.Object, []api.Issue) {
	if o.Kind() == api.Network {
		return normalizeNetworkArm(o), nil
	}
	if o.Kind() != api.InfraProvider {
		return o, nil
	}
	variant := Variant(o)
	if variant == "" {
		return o, nil
	}
	spec := o.Spec()
	arm := spec.Get(string(variant))
	if normalize, ok := armNormalizers[variant]; ok {
		arm = normalize(arm)
	}
	spec = spec.With(string(variant), arm)
	if normalize, ok := providerNormalizers[variant]; ok {
		spec = normalize(spec, arm, catalog)
	}
	return o.WithSpec(spec), nil
}

func ValidateAuthored(o api.Object, c api.Catalog) []api.Issue {
	if o.Kind() != api.InfraProvider {
		return nil
	}
	issues := validateHostedSegmentMTUs(o, c)
	for _, arm := range Arms() {
		if validate, ok := authoredValidators[arm]; ok && o.Spec().Has(string(arm)) {
			issues = api.AppendIssues(issues, validate(o)...)
		}
	}
	return issues
}

func Validate(o api.Object, c api.Catalog) []api.Issue {
	if o.Kind() == api.Machine {
		return validateFirmware(o)
	}
	if o.Kind() == api.Network {
		return validateNetworkArm(o, c)
	}
	if o.Kind() != api.InfraProvider {
		return nil
	}
	issues := []api.Issue{}
	variant := Variant(o)
	if variant == "" {
		return issues
	}
	arm := o.Spec().Get(string(variant))
	if validate, ok := armValidators[variant]; ok {
		issues = api.AppendIssues(issues, validate(o, c)...)
	}
	for index, segment := range o.Spec().Get("segments").Items() {
		path := fmt.Sprintf("$.spec.segments[%d]", index)
		if !segment.Has(string(variant)) {
			issues = api.AppendIssues(issues, issue(path, "segment arm must match its provider substrate"))
		}
		if validate, ok := segmentValidators[variant]; ok {
			issues = api.AppendIssues(issues, validate(path, segment, arm)...)
		}
		issues = api.AppendIssues(issues, validateCarriage(o, c, segment, variant, index)...)
	}
	issues = api.AppendIssues(issues, validateHostedProvider(o, c)...)
	return api.BoundIssues(issues)
}

func Variant(o api.Object) Arm {
	selected := Arm("")
	for _, name := range Arms() {
		if o.Spec().Has(string(name)) {
			if selected != "" {
				return ""
			}
			selected = name
		}
	}
	return selected
}

// requirePositiveCapacity refuses a profile size its arm cannot create a
// machine with. The schema materializes 0 for an omitted size, so an omitted
// size refuses here too.
func requirePositiveCapacity(arm api.Value, variant Arm, label string) []api.Issue {
	issues := []api.Issue{}
	for index, profile := range arm.Get("machineProfiles").Items() {
		for _, size := range []string{"cpu", "memoryMiB", "diskGiB"} {
			value := profile.Get(size)
			if value.Type() != api.Integer || value.Text() == "0" || strings.HasPrefix(value.Text(), "-") {
				issues = api.AppendIssues(issues, issue(fmt.Sprintf("$.spec.%s.machineProfiles[%d].%s", variant, index, size), label+" machine profiles require a positive capacity"))
			}
		}
	}
	return issues
}

func validateFirmware(o api.Object) []api.Issue {
	block := o.Spec().Get("substrate", "baremetal", "firmware")
	if !block.Present() {
		return nil
	}
	path := "$." + FirmwarePath
	refuse := func(field, message, remediation string) api.Issue {
		return api.Issue{Code: "api.invariant", Field: field, Message: message, Remediation: remediation}
	}
	issues := []api.Issue{}
	if !block.Has("secureBoot") && !block.Has("bootMode") && !block.Has("biosAttributes") {
		issues = api.AppendIssues(issues, refuse(path, "a firmware block must declare secureBoot, bootMode or biosAttributes", "set one of "+FirmwarePath+".secureBoot, bootMode or biosAttributes, or remove "+FirmwarePath))
	}
	if !MachineBMC(o).Present() {
		issues = api.AppendIssues(issues, refuse(path, "firmware settings are applied through the Machine's BMC and none is declared", "declare "+BMCPath+" beside "+FirmwarePath+", or remove "+FirmwarePath))
	}
	if o.Spec().Get("os", "provided").Bool() {
		issues = api.AppendIssues(issues, refuse(path, "an OS-ready Machine is not installed by Bootwright, so nothing applies its firmware block", "remove "+FirmwarePath+", or set os.provided: false so Bootwright installs the Machine"))
	}
	attributes := block.Get("biosAttributes")
	if !attributes.Present() {
		return issues
	}
	field := path + ".biosAttributes"
	entries := attributes.Fields()
	if len(entries) < 1 || len(entries) > FirmwareAttributes {
		issues = api.AppendIssues(issues, refuse(field, fmt.Sprintf("biosAttributes must declare 1 to %d entries, not %d", FirmwareAttributes, len(entries)), "declare between 1 and "+fmt.Sprint(FirmwareAttributes)+" BIOS attributes, or remove biosAttributes"))
	}
	for index, entry := range entries {
		at := fmt.Sprintf("%s entry %d (%s)", field, index+1, quotedName(entry.Name))
		if !firmwareAttributeName(entry.Name) {
			issues = api.AppendIssues(issues, refuse(field, fmt.Sprintf("%s: a name must start with a letter and hold 2 to %d letters, digits or underscores", at, FirmwareNameBytes), "rename the attribute to the exact name the BMC publishes in its Bios Attributes"))
		} else if strings.Contains(strings.ToLower(entry.Name), "password") {
			issues = api.AppendIssues(issues, refuse(field, at+": a firmware password is secret material and never desired state", "remove the attribute and set the password through the BMC's own interface"))
		}
		switch value := entry.Value; value.Type() {
		case api.String:
			if !firmwareString(value.Text()) {
				issues = api.AppendIssues(issues, refuse(field, fmt.Sprintf("%s: a string value must be at most %d bytes of printable ASCII", at, FirmwareStringBytes), "shorten the value or remove control and non-ASCII characters"))
			}
		case api.Integer:
			if _, ok := value.Int64(); !ok {
				issues = api.AppendIssues(issues, refuse(field, at+": an integer value must fit in 64 bits", "use an integer between -9223372036854775808 and 9223372036854775807"))
			}
		case api.Boolean:
		default:
			issues = api.AppendIssues(issues, refuse(field, at+": a value must be a string, an integer or a boolean", "declare the attribute as a string, an integer or a boolean, not a null, a float, a list or a map"))
		}
	}
	return issues
}

func quotedName(name string) string {
	if len(name) > 40 {
		name = name[:40] + "..."
	}
	return fmt.Sprintf("%q", name)
}

func firmwareAttributeName(name string) bool {
	if len(name) < 2 || len(name) > FirmwareNameBytes {
		return false
	}
	for index := 0; index < len(name); index++ {
		character := name[index]
		letter := character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
		if !letter && (index == 0 || character != '_' && (character < '0' || character > '9')) {
			return false
		}
	}
	return true
}

func firmwareString(value string) bool {
	if len(value) > FirmwareStringBytes {
		return false
	}
	for index := 0; index < len(value); index++ {
		if value[index] < 0x20 || value[index] > 0x7e {
			return false
		}
	}
	return true
}

const providerRemediation = "make the provider and its local selections consistent"

func issue(field, message string) api.Issue {
	return api.Issue{Code: "api.invariant", Field: field, Message: message, Remediation: providerRemediation}
}

func reference(field, message string) api.Issue {
	return api.ReferenceIssue(field, message, providerRemediation)
}

func ClientTLSExceptions(object api.Object, path string) bool {
	return object.Kind() == api.Machine && path == "$."+BMCPath+".tls"
}
