package machine

import (
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/substrate"
)

// ValidatePartial rejects contradictions already provable in unused defaults.
func ValidatePartial(o api.Object, c api.Catalog) []api.Issue {
	issues := []api.Issue{}
	if o.Kind() == api.NetworkConfig {
		return validateNative(o.Spec().Get("nmstate"), "$.spec.nmstate", true)
	}
	if o.Kind() != api.Machine {
		return issues
	}
	issues = api.AppendIssues(issues, infrastructureservices.ValidateProxyChoice(o.Spec().Get("proxy"), "$.spec.proxy")...)
	if installed(o) && o.Spec().Has("access") {
		issues = api.AppendIssues(issues, invariant("$.spec.access", "Bootwright-installed Machines must not author access", "remove access; Bootwright derives an installed Machine's access from the fleet key"))
	}
	issues = api.AppendIssues(issues, validateNative(o.Spec().Get("network", "inline", "nmstate"), "$.spec.network.inline.nmstate", true)...)
	issues = api.AppendIssues(issues, validateNative(o.Spec().Get("network", "overrides"), "$.spec.network.overrides", true)...)
	network := o.Spec().Get("network")
	if network.Has("inline") && network.Has("overrides") {
		issues = api.AppendIssues(issues, invariant("$.spec.network.overrides", "inline configuration forbids reusable-template overrides", "remove network.overrides, or select a reusable configuration with network.configRef instead of network.inline"))
	}
	if mode := substrate.MachineBMC(o).Get("tls", "verification").Text(); mode != "" && mode != "full" {
		issues = api.AppendIssues(issues, invariant("$."+substrate.BMCPath+".tls.verification", "non-full TLS verification is per-Machine intent and never a kind default", "declare tls.verification on each Machine that needs it"))
	}
	issues = api.AppendIssues(issues, validateDefaultMediaTrust(substrate.MachineBMC(o))...)
	issues = api.AppendIssues(issues, validateDefaultPerMachineHardware(o)...)
	if api.MachineProvided(o) {
		if o.Spec().Has("os", "install", "ntp") {
			issues = api.AppendIssues(issues, invariant("$.spec.os.install.ntp", "OS-ready Machines forbid installation NTP choices", "remove os.install.ntp"))
		}
		for _, key := range []string{"configRef", "inline", "interfaceMatches", "installAddressRef", "overrides"} {
			if network.Has(key) {
				issues = api.AppendIssues(issues, invariant("$.spec.network."+key, "OS-ready Machines declare contacts only", "remove network."+key+"; an OS-ready Machine declares contacts only"))
			}
		}
	}
	return issues
}

// validateDefaultMediaTrust refuses the virtual-media exception in a Machine
// kind default: inherited, it would reach every Machine that authors no trust.
func validateDefaultMediaTrust(bmc api.Value) []api.Issue {
	if bmc.Get("virtualMedia", "tls", "trust").Text() != substrate.TrustDisableVerification {
		return nil
	}
	return []api.Issue{{Code: "api.invariant", Field: "$." + substrate.BMCPath + ".virtualMedia.tls.trust",
		Message:     "disableVerification is a per-Machine exception and never a kind default",
		Remediation: "declare hardware.bmc.virtualMedia.tls.trust: disableVerification on each Machine that needs it"}}
}

func validateDefaultPerMachineHardware(o api.Object) []api.Issue {
	issues := []api.Issue{}
	if substrate.MachineFirmware(o) != nil {
		issues = append(issues, perMachineIntent("$."+substrate.FirmwarePath, "firmware"))
	}
	if substrate.MachineLinuxPreflight(o).Present() {
		issues = append(issues, perMachineIntent("$."+substrate.RootDiskPath+".linuxPreflight", "rootDisk.linuxPreflight"))
	}
	if substrate.MachineRootDisk(o).Has("onRemoval") {
		issues = append(issues, perMachineIntent("$."+substrate.RootDiskPath+".onRemoval", "rootDisk.onRemoval"))
	}
	if o.Spec().Has("hardware", "dataDisks") {
		issues = append(issues, perMachineIntent("$."+substrate.DataDisksPath, "dataDisks"))
	}
	return issues
}

func perMachineIntent(field, name string) api.Issue {
	return api.Issue{Code: "api.invariant", Field: field,
		Message:     name + " is per-Machine intent and never a kind default",
		Remediation: "declare hardware." + name + " on each Machine that needs it"}
}
