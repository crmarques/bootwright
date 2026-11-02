package main

import (
	"context"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/storage/ceph"
	"github.com/crmarques/bootwright/internal/storage/preflight"
)

const (
	hostPortCheck    = "host-port"
	hostPortRealized = "realized"
	hostPortManual   = "manual"
)

func hostPortRequirements(catalog api.Catalog) ([]installation.HostPortRequirement, error) {
	published, err := ceph.HostPortRequirements(catalog)
	if err != nil {
		return nil, err
	}
	requirements := make([]installation.HostPortRequirement, 0, len(published))
	for _, requirement := range published {
		ports := make([]installation.HostPort, 0, len(requirement.Ports))
		for _, port := range requirement.Ports {
			ports = append(ports, installation.HostPort{Number: port.Number, Transport: port.Transport})
		}
		requirements = append(requirements, installation.HostPortRequirement{Machine: requirement.Machine, Publisher: requirement.Publisher, Networks: slices.Clone(requirement.Networks), Protocols: slices.Clone(requirement.Protocols), Ports: ports})
	}
	return requirements, nil
}

func preflightHostPorts(catalog api.Catalog) ([]preflight.HostPortRequirement, error) {
	published, err := ceph.HostPortRequirements(catalog)
	if err != nil {
		return nil, err
	}
	requirements := make([]preflight.HostPortRequirement, 0, len(published))
	for _, requirement := range published {
		ports := make([]preflight.HostPort, 0, len(requirement.Ports))
		for _, port := range requirement.Ports {
			ports = append(ports, preflight.HostPort{Number: port.Number, Transport: port.Transport})
		}
		requirements = append(requirements, preflight.HostPortRequirement{Machine: requirement.Machine, Publisher: requirement.Publisher, Networks: slices.Clone(requirement.Networks), Protocols: slices.Clone(requirement.Protocols), Ports: ports})
	}
	return requirements, nil
}

func hostPortPlanInspection(requirements installation.HostPortRequirements, next lifecycle.PlanInspection) lifecycle.PlanInspection {
	return func(ctx context.Context, name string, catalog api.Catalog, plan reconciliation.Plan, starting []reconciliation.Block) ([]lifecycle.PlanCheck, error) {
		checks, err := next(ctx, name, catalog, plan, starting)
		if err != nil {
			return nil, err
		}
		rows, err := hostPortChecks(catalog, requirements)
		if err != nil {
			return nil, err
		}
		return append(checks, rows...), nil
	}
}

func hostPortChecks(catalog api.Catalog, requirements installation.HostPortRequirements) ([]lifecycle.PlanCheck, error) {
	if requirements == nil {
		return nil, nil
	}
	found, err := requirements(catalog)
	if err != nil {
		return nil, err
	}
	rows := make([]lifecycle.PlanCheck, 0, len(found))
	for _, requirement := range found {
		machine := "Machine/" + requirement.Machine
		admits := hostPortAdmission(requirement)
		networks := hostPortInterface(requirement.Networks)
		summary := machine + " admits " + admits + " on " + networks + " for " + requirement.Publisher
		if installation.RealizesHostPorts(catalog, requirement.Machine) {
			rows = append(rows, lifecycle.PlanCheck{ID: hostPortCheck, Status: hostPortRealized, Summary: summary + ": realized by the managed OS before the clusters stage"})
			continue
		}
		remedy := "permit " + admits + " in the firewalld zone of " + networks + " of " + machine
		if profile, installed := installedProfile(catalog, requirement.Machine); installed {
			remedy += ", or set spec.customizations.security.firewall.enabled: true on MachineInstallProfile/" + profile + " so the managed OS realizes it"
		}
		rows = append(rows, lifecycle.PlanCheck{ID: hostPortCheck, Status: hostPortManual, Summary: summary + ": the host's own firewall policy must admit it", Remedy: remedy})
	}
	return rows, nil
}

func hostPortAdmission(requirement installation.HostPortRequirement) string {
	var admitted []string
	for _, port := range requirement.Ports {
		admitted = append(admitted, strconv.Itoa(port.Number)+"/"+port.Transport)
	}
	if len(requirement.Protocols) != 0 {
		admitted = append(admitted, "IP protocol "+strings.Join(requirement.Protocols, ", "))
	}
	return strings.Join(admitted, ", ")
}

func installedProfile(catalog api.Catalog, machine string) (string, bool) {
	for _, object := range installation.InstalledMachines(catalog) {
		if object.Name() == machine {
			return object.Spec().Get("os", "installProfileRef").Text(), true
		}
	}
	return "", false
}

func hostPortInterface(networks []string) string {
	if len(networks) == 0 {
		return "no interface"
	}
	named := "the interface whose network holds " + strings.TrimSuffix(strings.TrimSuffix(networks[0], "/32"), "/128")
	if len(networks) > 1 {
		named += ", else the interface on " + strings.Join(networks[1:], ", ")
	}
	return named
}
