package cli

import (
	"strings"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// Ambiguous frozen actions retain their authoritative description.
func lifecycleAction(step lifecycle.PlanStep, verb string) (label, target string) {
	fallback := func() (string, string) { return step.Description, "" }
	if step.Object == "" || (verb != "apply" && verb != "destroy") {
		return fallback()
	}
	var apply, destroy string
	switch step.Kind {
	case "DNSServer":
		if step.Implementation == "dns-server-dnsmasq-v1" {
			apply, destroy = "Start DNS service", "Stop DNS service"
		}
	case "NTPServer":
		if step.Implementation == "ntp-server-chrony-v1" {
			apply, destroy = "Start time service", "Stop time service"
		}
	case "ArtifactServer":
		if step.Implementation == "artifact-server-nginx-v1" {
			apply, destroy = "Serve installer artifacts", "Stop artifact service"
		}
	case "Proxy":
		if step.Implementation == "proxy-squid-v1" {
			apply, destroy = "Start proxy service", "Stop proxy service"
		}
	case "Machine":
		switch step.Implementation {
		case "machine-libvirt-v1":
			apply, destroy = "Create virtual machine and controller", "Remove virtual machine and controller"
		case "machine-baremetal-v1":
			apply = "Claim physical machine and prove identity"
		case "os-install-anaconda-v1":
			apply, destroy = "Install operating system", "Remove installer media"
		}
	case "ContainerCluster":
		switch step.Implementation {
		case "cluster-media-agent-v1":
			apply, destroy = "Build boot media", "Remove boot media"
		case "cluster-install-agent-v1":
			apply, destroy = "Install container cluster", "Release boot media"
		}
	case "StorageCluster":
		if step.Implementation == "storage-ceph-image-v1" {
			apply, destroy = "Resolve Ceph image", "Release Ceph image"
		}
	case "MachineInstallProfile":
		if step.Implementation == "os-package-tree-v1" {
			apply, destroy = "Publish installer package tree", "Remove installer package tree"
		}
	}
	label = apply
	if verb == "destroy" {
		label = destroy
	}
	if label == "" {
		return fallback()
	}
	return label, step.Object
}

func lifecycleActionLabel(label, target, identity string, opts PresentationOptions) string {
	text := escapeDisplayLine(label)
	if target != "" {
		return text + " | " + styleIdentity(escapeDisplayLine(target), opts)
	}
	if identity != "" {
		escaped := escapeDisplayLine(identity)
		text = strings.ReplaceAll(text, escaped, styleIdentity(escaped, opts))
	}
	return text
}
