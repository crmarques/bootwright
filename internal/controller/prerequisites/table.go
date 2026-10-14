package prerequisites

import (
	"fmt"
	"strings"
)

type ReceiptTable struct {
	Host     string
	Runtime  string
	Packages string
}

func (i inspection) table(lock RuntimeLock) *ReceiptTable {
	return &ReceiptTable{Host: i.hostLine(lock), Runtime: i.runtimeLine(lock), Packages: i.packagesLine()}
}

func (i inspection) hostLine(lock RuntimeLock) string {
	release := i.release
	line := fmt.Sprintf("%s %s (%s)", hostName(release.ID), release.VersionID, release.Architecture)
	if i.interpreter.Glibc != "" {
		line += ", glibc " + i.interpreter.Glibc
	}
	switch {
	case len(lock.Tested) == 0:
	case tested(release, lock.Tested):
		line += " — tested"
	default:
		line += " — untested"
	}
	return line
}

func hostName(id string) string {
	switch id {
	case "fedora":
		return "Fedora Linux"
	case "rhel":
		return "Red Hat Enterprise Linux"
	}
	return id
}

func (i inspection) runtimeLine(lock RuntimeLock) string {
	line, core, status := "Python "+i.abi+" (not installed)", lock.AnsibleCore, "not prepared"
	if i.interpreter.Path != "" {
		line = "Python " + i.interpreter.Version + " (" + i.interpreter.Path + ")"
	}
	if i.runtimeReady {
		core, status = i.runtime.AnsibleCore, "ready"
	}
	return line + ", ansible-core " + core + " — " + status
}

func (i inspection) packagesLine() string {
	receipt := i.view.State.Receipt
	switch {
	case len(i.missing) != 0:
		return "missing " + strings.Join(i.missing, ", ")
	case receipt == nil || len(receipt.Packages) == 0:
		return "present, not yet recorded by setup"
	}
	items := make([]string, 0, len(receipt.Packages))
	for _, item := range receipt.Packages {
		items = append(items, providerDisplay(item.Provider)+" ("+packageOrigin(item)+")")
	}
	return strings.Join(items, ", ")
}

func packageOrigin(item ReceiptPackage) string {
	if item.InstalledBy == PresentBeforeBootwright || item.Repository == unknownRepo || item.Repository == "" {
		return item.InstalledBy
	}
	return item.Repository
}

func providerDisplay(nevra string) string {
	release := strings.LastIndex(nevra, "-")
	if release < 0 {
		return nevra
	}
	name := strings.LastIndex(nevra[:release], "-")
	if name < 0 {
		return nevra
	}
	return nevra[:name] + " " + nevra[name+1:]
}
