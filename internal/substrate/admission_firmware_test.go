package substrate

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

const firmwareField = "$.spec.substrate.baremetal.firmware"
const attributesField = firmwareField + ".biosAttributes"

func firmwareMachine(provided bool, bmc bool, firmware api.Value) api.Object {
	arm := m("firmware", firmware)
	if bmc {
		arm = arm.With("bmc", m("address", "https://bmc.example.test/redfish/v1/Systems/1", "credentialsRef", "bmc"))
	}
	return obj(api.Machine, "node", m("os", m("provided", provided), "substrate", m("baremetal", arm)))
}

func attributes(entries ...any) api.Value { return m(entries...) }

func manyAttributes(count int) api.Value {
	entries := []any{}
	for index := range count {
		entries = append(entries, "Attribute"+strconv.Itoa(index), true)
	}
	return attributes(entries...)
}

func TestFirmwareAdmission(t *testing.T) {
	declared := func(entries ...any) api.Value { return m("mode", "enforce", "biosAttributes", attributes(entries...)) }
	for _, test := range []struct {
		name     string
		machine  api.Object
		field    string
		contains string
	}{
		{"an empty block", firmwareMachine(false, true, m()), firmwareField, "must declare secureBoot, bootMode or biosAttributes"},
		{"a mode alone", firmwareMachine(false, true, m("mode", "prove")), firmwareField, "must declare secureBoot, bootMode or biosAttributes"},
		{"an empty attribute map", firmwareMachine(false, true, m("biosAttributes", attributes())), attributesField, "1 to 64 entries, not 0"},
		{"sixty-five attributes", firmwareMachine(false, true, m("biosAttributes", manyAttributes(65))), attributesField, "1 to 64 entries, not 65"},
		{"a name starting with a digit", firmwareMachine(false, true, declared("9x", true)), attributesField, "entry 1 (\"9x\"): a name must start with a letter"},
		{"a name with a dash", firmwareMachine(false, true, declared("Fine", true, "a-b", true)), attributesField, "entry 2 (\"a-b\")"},
		{"a one-character name", firmwareMachine(false, true, declared("a", true)), attributesField, "a name must start with a letter"},
		{"a name over 128 bytes", firmwareMachine(false, true, declared("A"+strings.Repeat("b", 128), true)), attributesField, "a name must start with a letter"},
		{"a 257-byte string", firmwareMachine(false, true, declared("Banner", strings.Repeat("x", 257))), attributesField, "at most 256 bytes of printable ASCII"},
		{"a control character", firmwareMachine(false, true, declared("Banner", "line\nbreak")), attributesField, "printable ASCII"},
		{"a non-ASCII string", firmwareMachine(false, true, declared("Banner", "café")), attributesField, "printable ASCII"},
		{"a float", firmwareMachine(false, true, declared("Ratio", api.NumberValue("1.5"))), attributesField, "a string, an integer or a boolean"},
		{"a null", firmwareMachine(false, true, declared("Ratio", api.Value{})), attributesField, "a string, an integer or a boolean"},
		{"a list", firmwareMachine(false, true, declared("Ratio", api.ListValue(api.IntegerValue("1")))), attributesField, "a string, an integer or a boolean"},
		{"a map", firmwareMachine(false, true, declared("Ratio", m("a", "b"))), attributesField, "a string, an integer or a boolean"},
		{"an integer beyond 64 bits", firmwareMachine(false, true, declared("Big", api.IntegerValue("9223372036854775808"))), attributesField, "fit in 64 bits"},
		{"a password-style name", firmwareMachine(false, true, declared("AdminPassword", "hunter2")), attributesField, "secret material"},
		{"a lower-case password name", firmwareMachine(false, true, declared("setup_password_enabled", true)), attributesField, "secret material"},
		{"an OS-ready Machine", firmwareMachine(true, true, m("secureBoot", true)), firmwareField, "nothing applies its firmware block"},
		{"no BMC", firmwareMachine(false, false, m("secureBoot", true)), firmwareField, "none is declared"},
	} {
		t.Run(test.name, func(t *testing.T) {
			issues := Validate(test.machine, api.NewCatalog([]api.Object{test.machine}))
			found := slices.IndexFunc(issues, func(issue api.Issue) bool {
				return issue.Field == test.field && issue.Code == "api.invariant" && strings.Contains(issue.Message, test.contains) && issue.Remediation != ""
			})
			if found < 0 {
				t.Fatalf("issues = %#v, want %s containing %q", issues, test.field, test.contains)
			}
			for _, issue := range issues {
				if strings.Contains(issue.Message, "hunter2") {
					t.Fatalf("a value reached a diagnostic: %q", issue.Message)
				}
			}
		})
	}
}

func TestFirmwareAdmissionAdmitsADeclaredBlock(t *testing.T) {
	for name, firmware := range map[string]api.Value{
		"secure boot alone": m("secureBoot", false),
		"boot mode alone":   m("bootMode", "UEFI"),
		"a prove block":     m("mode", "prove", "secureBoot", true, "bootMode", "UEFI"),
		"every value kind": m("biosAttributes", attributes("TpmState", true, "Banner", strings.Repeat("x", 256), "Timeout", api.IntegerValue("30"),
			"Min", api.IntegerValue(strconv.FormatInt(math.MinInt64, 10)), "Max", api.IntegerValue(strconv.FormatInt(math.MaxInt64, 10)), "Empty", "")),
		"sixty-four attributes": m("biosAttributes", manyAttributes(64)),
	} {
		t.Run(name, func(t *testing.T) {
			machine := firmwareMachine(false, true, firmware)
			if issues := Validate(machine, api.NewCatalog([]api.Object{machine})); len(issues) != 0 {
				t.Fatalf("a valid firmware block was refused: %#v", issues)
			}
		})
	}
	absent := obj(api.Machine, "node", m("os", m("provided", true), "substrate", m("baremetal", m())))
	if issues := Validate(absent, api.NewCatalog([]api.Object{absent})); len(issues) != 0 {
		t.Fatalf("an absent block refused: %#v", issues)
	}
}

func TestMachineFirmwareReader(t *testing.T) {
	if got := MachineFirmware(firmwareMachineWithout()); got != nil {
		t.Fatalf("an absent block read %+v", got)
	}
	if got := MachineFirmware(obj(api.Machine, "node", m())); got != nil {
		t.Fatalf("a Machine without a substrate read %+v", got)
	}
	machine := firmwareMachine(false, true, m("secureBoot", false, "bootMode", "UEFI", "biosAttributes", attributes(
		"Zeta", "last", "Min", api.IntegerValue(strconv.FormatInt(math.MinInt64, 10)), "Alpha", true, "Max", api.IntegerValue(strconv.FormatInt(math.MaxInt64, 10)), "Empty", "")))
	got := MachineFirmware(machine)
	if got == nil || got.Mode != FirmwareModeEnforce || got.BootMode != "UEFI" || got.SecureBoot == nil || *got.SecureBoot {
		t.Fatalf("firmware = %+v", got)
	}
	names := []string{}
	for _, attribute := range got.BIOSAttributes {
		names = append(names, attribute.Name)
	}
	if !slices.Equal(names, []string{"Alpha", "Empty", "Max", "Min", "Zeta"}) {
		t.Fatalf("attributes are not sorted by name: %v", names)
	}
	byName := map[string]BIOSAttribute{}
	for _, attribute := range got.BIOSAttributes {
		byName[attribute.Name] = attribute
	}
	if a := byName["Alpha"]; a.Boolean == nil || !*a.Boolean || a.Integer != nil || a.String != nil {
		t.Fatalf("Alpha = %+v", a)
	}
	if a := byName["Max"]; a.Integer == nil || *a.Integer != math.MaxInt64 || a.Boolean != nil || a.String != nil {
		t.Fatalf("Max = %+v", a)
	}
	if a := byName["Min"]; a.Integer == nil || *a.Integer != math.MinInt64 {
		t.Fatalf("Min = %+v", a)
	}
	if a := byName["Empty"]; a.String == nil || *a.String != "" || a.Integer != nil || a.Boolean != nil {
		t.Fatalf("Empty = %+v", a)
	}
	if a := byName["Zeta"]; a.String == nil || *a.String != "last" {
		t.Fatalf("Zeta = %+v", a)
	}
	proving := MachineFirmware(firmwareMachine(false, true, m("mode", "prove", "secureBoot", true)))
	if proving == nil || proving.Mode != FirmwareModeProve || proving.SecureBoot == nil || !*proving.SecureBoot || proving.BIOSAttributes != nil {
		t.Fatalf("prove firmware = %+v", proving)
	}
}

func firmwareMachineWithout() api.Object {
	return obj(api.Machine, "node", m("os", m("provided", false), "substrate", m("baremetal", m("bmc", m("address", "https://bmc.example.test/redfish/v1/Systems/1")))))
}
