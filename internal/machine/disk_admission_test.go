package machine

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/substrate"
)

const (
	diskRootWWN    = "0x5000c500a1b2c3d4"
	diskRootSerial = "SYNTHETIC-ROOT-01"
	diskRootSize   = "480103981056"
	diskDataSize   = "107374182400"
	diskBound      = "962072674304"
	diskOverBound  = "962072678400"
)

func diskBMC(verification, trust string) api.Value {
	bmc := m("address", "https://bmc.example.test/redfish/v1/Systems/1", "credentialsRef", "bmc")
	if verification != "" {
		bmc = bmc.With("tls", m("verification", verification))
	}
	if trust != "" {
		bmc = bmc.With("virtualMedia", m("tls", m("trust", trust)))
	}
	return bmc
}

func diskRoot(onRemoval string) api.Value {
	root := m("wwn", diskRootWWN, "serialNumber", diskRootSerial).With("sizeBytes", api.IntegerValue(diskRootSize))
	if onRemoval != "" {
		root = root.With("onRemoval", api.StringValue(onRemoval))
	}
	return root
}

func dataDisk(wwnDigits, serial, onRemoval string) api.Value {
	disk := m("serialNumber", serial).With("sizeBytes", api.IntegerValue(diskDataSize))
	if wwnDigits != "" {
		disk = disk.With("wwn", api.StringValue("0x"+wwnDigits))
	}
	if onRemoval != "" {
		disk = disk.With("onRemoval", api.StringValue(onRemoval))
	}
	return disk
}

func clearedDisk(wwnDigits, serial string) api.Value {
	return dataDisk(wwnDigits, serial, "").With("onInstall", api.StringValue("metadataClear"))
}

func diskMachine(root api.Value, disks ...api.Value) api.Object {
	hardware := m("providerRef", "metal", "bmc", diskBMC("", "established"),
		"nics", list(m("name", "eth0", "macAddress", "02:00:00:00:00:01")), "rootDisk", root)
	if len(disks) != 0 {
		hardware = hardware.With("dataDisks", list(disks...))
	}
	return object(api.Machine, "node", m("hardware", hardware, "os", m("install", m("profileRef", "rhel", "hostKeyRef", "host-key"))))
}

func diskSet(o api.Object, value api.Value, path ...string) api.Object {
	return o.WithSpec(o.Spec().WithPath(value, path...))
}

func physicalDisks(o api.Object) []api.Issue { return validateDisks(o, true) }

func diskNoIssues(t *testing.T, issues []api.Issue) {
	t.Helper()
	if len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
}

func diskOneIssue(t *testing.T, issues []api.Issue, field string) api.Issue {
	t.Helper()
	if len(issues) != 1 || issues[0].Field != field || issues[0].Code != "api.invariant" {
		t.Fatalf("issues = %+v, want one api.invariant at %s", issues, field)
	}
	return issues[0]
}

func diskIssueAt(t *testing.T, issues []api.Issue, field string) api.Issue {
	t.Helper()
	for _, issue := range issues {
		if issue.Field == field && issue.Code == "api.invariant" {
			return issue
		}
	}
	t.Fatalf("no api.invariant at %s: %+v", field, issues)
	return api.Issue{}
}

func TestDiskAdmissionRetainsByOmissionAndAdmitsErasureByDeclaredIdentity(t *testing.T) {
	for _, onRemoval := range []string{"", "retain", "metadataClear", "logicalOverwrite"} {
		diskNoIssues(t, physicalDisks(diskMachine(diskRoot(onRemoval))))
	}
	bySerial := m("serialNumber", diskRootSerial).With("sizeBytes", api.IntegerValue(diskRootSize)).With("onRemoval", api.StringValue("logicalOverwrite"))
	diskNoIssues(t, physicalDisks(diskMachine(bySerial)))
	diskNoIssues(t, physicalDisks(diskMachine(diskRoot("metadataClear"), dataDisk("5000000000000001", "DATA-01", "logicalOverwrite"), clearedDisk("5000000000000002", "DATA-02"))))
	diskNoIssues(t, physicalDisks(diskMachine(diskRoot(""), dataDisk("5000000000000001", "DATA-01", "metadataClear"))))
	diskNoIssues(t, physicalDisks(diskMachine(diskRoot(""), dataDisk("", "DATA-01", "metadataClear"))))
}

func TestDiskAdmissionRefusesRemovalAndDataDisksOffAnInstalledPhysicalMachine(t *testing.T) {
	erasing := diskMachine(diskRoot("logicalOverwrite"))
	data := diskMachine(diskRoot(""), clearedDisk("5000000000000001", "DATA-01"))
	diskOneIssue(t, validateDisks(erasing, false), "$.spec.hardware.rootDisk.onRemoval")
	diskOneIssue(t, validateDisks(data, false), "$.spec.hardware.dataDisks")
	diskNoIssues(t, validateDisks(diskMachine(diskRoot("retain")), false))
	clusterInstalled := m("clusterInstalled", m())
	diskOneIssue(t, physicalDisks(diskSet(erasing, clusterInstalled, "os")), "$.spec.hardware.rootDisk.onRemoval")
	diskOneIssue(t, physicalDisks(diskSet(data, clusterInstalled, "os")), "$.spec.hardware.dataDisks")
	diskNoIssues(t, physicalDisks(diskSet(diskMachine(diskRoot("retain")), clusterInstalled, "os")))
}

func TestDiskAdmissionRefusesOnRemovalAndDataDisksOnAnOSReadyMachine(t *testing.T) {
	provided := m("provided", m())
	for _, onRemoval := range []string{"retain", "metadataClear", "logicalOverwrite"} {
		issue := diskOneIssue(t, physicalDisks(diskSet(diskMachine(diskRoot(onRemoval)), provided, "os")), "$.spec.hardware.rootDisk.onRemoval")
		if !strings.Contains(issue.Message, "OS-ready") {
			t.Fatalf("%s: issue = %+v", onRemoval, issue)
		}
	}
	diskNoIssues(t, physicalDisks(diskSet(diskMachine(diskRoot("")), provided, "os")))
	diskOneIssue(t, physicalDisks(diskSet(diskMachine(diskRoot(""), clearedDisk("5000000000000001", "DATA-01")), provided, "os")), "$.spec.hardware.dataDisks")
}

func TestRootRemovalRequiresTheRootsSerialNumberAndSize(t *testing.T) {
	for name, test := range map[string]struct {
		root    api.Value
		missing string
	}{
		"no serialNumber": {diskRoot("metadataClear").Without("serialNumber"), "no serialNumber"},
		"no sizeBytes":    {diskRoot("metadataClear").Without("sizeBytes"), "no sizeBytes"},
		"neither":         {diskRoot("metadataClear").Without("serialNumber").Without("sizeBytes"), "neither"},
		"a lone wwn":      {m("wwn", diskRootWWN, "onRemoval", "logicalOverwrite"), "neither"},
	} {
		t.Run(name, func(t *testing.T) {
			issue := diskOneIssue(t, physicalDisks(diskMachine(test.root)), "$.spec.hardware.rootDisk.onRemoval")
			if !strings.Contains(issue.Message, "declares "+test.missing) || !strings.Contains(issue.Remediation, "rootDisk.serialNumber and sizeBytes") {
				t.Fatalf("issue = %+v", issue)
			}
			diskNoIssues(t, physicalDisks(diskSet(diskMachine(test.root), api.StringValue("retain"), "hardware", "rootDisk", "onRemoval")))
		})
	}
}

func TestRootRemovalRefusesBesideASelectorThatDeclaresNoIdentity(t *testing.T) {
	volume := m("storageId", "RAID-Integrated-1", "volumeId", "Disk_Virtual_0")
	for selector, value := range map[string]api.Value{"volumeRef": volume, "bootVolume": m(), "discover": m()} {
		t.Run(selector, func(t *testing.T) {
			erasing := diskRoot("metadataClear").Without("wwn").With(selector, value)
			issue := diskOneIssue(t, physicalDisks(diskMachine(erasing)), "$.spec.hardware.rootDisk.onRemoval")
			if !strings.Contains(issue.Message, selector) {
				t.Fatalf("issue = %+v", issue)
			}
			retained := diskRoot("").Without("wwn").With(selector, value)
			diskNoIssues(t, physicalDisks(diskMachine(retained)))
		})
	}
}

func TestDiskAdmissionBoundsOnlyLogicalOverwriteTo896GiB(t *testing.T) {
	if MaxLogicalOverwriteBytes != 896<<30 {
		t.Fatal("the logicalOverwrite bound moved without its spec")
	}
	rootOf := func(method, size string) api.Object {
		return diskMachine(diskRoot(method).With("sizeBytes", api.IntegerValue(size)))
	}
	dataOf := func(method, size string) api.Object {
		return diskMachine(diskRoot(""), dataDisk("5000000000000001", "DATA-01", method).With("sizeBytes", api.IntegerValue(size)))
	}
	diskNoIssues(t, physicalDisks(rootOf("logicalOverwrite", diskBound)))
	diskNoIssues(t, physicalDisks(rootOf("metadataClear", diskOverBound)))
	issue := diskOneIssue(t, physicalDisks(rootOf("logicalOverwrite", diskOverBound)), "$.spec.hardware.rootDisk.sizeBytes")
	if !strings.Contains(issue.Remediation, "metadataClear") {
		t.Fatalf("issue = %+v", issue)
	}
	diskNoIssues(t, physicalDisks(dataOf("logicalOverwrite", diskBound)))
	diskNoIssues(t, physicalDisks(dataOf("metadataClear", diskOverBound)))
	issue = diskOneIssue(t, physicalDisks(dataOf("logicalOverwrite", diskOverBound)), "$.spec.hardware.dataDisks[0].sizeBytes")
	if !strings.Contains(issue.Remediation, "metadataClear") {
		t.Fatalf("issue = %+v", issue)
	}
	pair := diskMachine(diskRoot(""), dataDisk("5000000000000001", "DATA-01", "metadataClear"), dataDisk("5000000000000002", "DATA-02", "logicalOverwrite").With("sizeBytes", api.IntegerValue(diskOverBound)))
	diskOneIssue(t, physicalDisks(pair), "$.spec.hardware.dataDisks[1].sizeBytes")
}

func TestErasureRequiresVerifiedBMCTLS(t *testing.T) {
	noBMC := func(o api.Object) api.Object {
		return o.WithSpec(o.Spec().WithPath(o.Spec().Get("hardware").Without("bmc"), "hardware"))
	}
	// A case named unnormalized skips NormalizeBMCDefaults, so its absent trust
	// stays absent; admission always runs after normalization fills it.
	for name, bmc := range map[string]api.Value{
		"verification none":               diskBMC("none", "established"),
		"unnormalized, no media trust":    diskBMC("full", ""),
		"verification none, no trust":     diskBMC("none", ""),
		"unnormalized, default tls":       diskBMC("", ""),
		"verification none, import trust": diskBMC("none", "importCertificate"),
	} {
		t.Run(name, func(t *testing.T) {
			root := diskSet(diskMachine(diskRoot("logicalOverwrite")), bmc, "hardware", "bmc")
			issue := diskOneIssue(t, physicalDisks(root), "$.spec.hardware.rootDisk.onRemoval")
			if issue.Message != "physical erase requires verified BMC TLS" || !strings.Contains(issue.Remediation, "https://") || strings.Contains(issue.Remediation, "tls.trust") {
				t.Fatalf("issue = %+v", issue)
			}
			data := diskSet(diskMachine(diskRoot(""), dataDisk("5000000000000001", "DATA-01", "metadataClear")), bmc, "hardware", "bmc")
			diskOneIssue(t, physicalDisks(data), "$.spec.hardware.dataDisks[0].onRemoval")
			clearing := diskSet(diskMachine(diskRoot(""), clearedDisk("5000000000000001", "DATA-01")), bmc, "hardware", "bmc")
			diskNoIssues(t, physicalDisks(clearing))
			retained := diskSet(diskMachine(diskRoot("retain")), bmc, "hardware", "bmc")
			diskNoIssues(t, physicalDisks(retained))
		})
	}
	diskOneIssue(t, physicalDisks(noBMC(diskMachine(diskRoot("metadataClear")))), "$.spec.hardware.rootDisk.onRemoval")
	diskOneIssue(t, physicalDisks(noBMC(diskMachine(diskRoot(""), dataDisk("5000000000000001", "DATA-01", "metadataClear")))), "$.spec.hardware.dataDisks[0].onRemoval")
	for _, verification := range []string{"full", "certificate"} {
		for _, trust := range []string{"established", "importCertificate", "disableVerification"} {
			machine := diskSet(diskMachine(diskRoot("logicalOverwrite"), dataDisk("5000000000000001", "DATA-01", "metadataClear")), diskBMC(verification, trust), "hardware", "bmc")
			diskNoIssues(t, physicalDisks(machine))
		}
	}
	unverified := diskSet(diskMachine(diskRoot(""),
		dataDisk("5000000000000001", "DATA-01", "metadataClear"),
		dataDisk("5000000000000002", "DATA-02", "metadataClear"),
		dataDisk("5000000000000003", "DATA-03", "logicalOverwrite")), diskBMC("none", "established"), "hardware", "bmc")
	diskOneIssue(t, physicalDisks(unverified), "$.spec.hardware.dataDisks[0].onRemoval")
}

func TestErasureAcceptsTheDefaultedMediaTrustOfAVerifiedBMC(t *testing.T) {
	bmc := substrate.NormalizeBMCDefaults(diskBMC("full", ""))
	if trust := bmc.Get("virtualMedia", "tls", "trust").Text(); trust != "importCertificate" {
		t.Fatalf("normalized trust = %q", trust)
	}
	diskNoIssues(t, physicalDisks(diskSet(diskMachine(diskRoot("logicalOverwrite")), bmc, "hardware", "bmc")))
	unverified := substrate.NormalizeBMCDefaults(diskBMC("none", ""))
	diskOneIssue(t, physicalDisks(diskSet(diskMachine(diskRoot("logicalOverwrite")), unverified, "hardware", "bmc")), "$.spec.hardware.rootDisk.onRemoval")
}

// A plain-HTTP BMC carries no TLS leg, so it is unverified whatever its
// verification field says, and a physical erase over it refuses.
func TestEraseRefusesAPlainHTTPBMC(t *testing.T) {
	for _, trust := range []string{"established", "importCertificate", "disableVerification"} {
		plain := diskBMC("", trust).With("address", api.StringValue("http://bmc.example.test/redfish/v1/Systems/1"))
		root := diskSet(diskMachine(diskRoot("logicalOverwrite")), plain, "hardware", "bmc")
		issue := diskOneIssue(t, physicalDisks(root), "$.spec.hardware.rootDisk.onRemoval")
		if !strings.Contains(issue.Message, "verified BMC") || !strings.Contains(issue.Remediation, "https://") {
			t.Fatalf("issue = %+v", issue)
		}
		data := diskSet(diskMachine(diskRoot(""), dataDisk("5000000000000001", "DATA-01", "metadataClear")), plain, "hardware", "bmc")
		diskOneIssue(t, physicalDisks(data), "$.spec.hardware.dataDisks[0].onRemoval")
		diskNoIssues(t, physicalDisks(diskSet(diskMachine(diskRoot("retain")), plain, "hardware", "bmc")))
	}
}

func TestADataDiskSerialNumberIsWithinTheDiskIdentityGrammar(t *testing.T) {
	for serial, admitted := range map[string]bool{
		"EXAMPLE-01": true, "Z1.2:3+4/5=6": true, "A": true, strings.Repeat("A", 256): true,
		"EXAMPLE 01": false, "EXAMPLE<01": false, `EXAMPLE"01`: false, "": false, "é": false, strings.Repeat("A", 257): false,
	} {
		machine := diskMachine(diskRoot(""), dataDisk("5000000000000001", serial, "metadataClear"))
		issues := physicalDisks(machine)
		if admitted {
			diskNoIssues(t, issues)
			continue
		}
		diskOneIssue(t, issues, "$.spec.hardware.dataDisks[0].serialNumber")
	}
}

func TestADataDiskWWNIsWithinTheRootWWNGrammar(t *testing.T) {
	for wwn, admitted := range map[string]bool{
		"0x5000000000000001":                 true,
		"0x6000c500a1b2c3d4e5f60718293a4b5c": true,
		"0X5000000000000001":                 false,
		"5000000000000001":                   false,
		"0x50000000":                         false,
		"0x5000000000000ABC":                 false,
		"":                                   false,
		"naa.5000000000000001":               false,
	} {
		machine := diskMachine(diskRoot(""), dataDisk("", "DATA-01", "metadataClear").With("wwn", api.StringValue(wwn)))
		issues := physicalDisks(machine)
		if admitted {
			diskNoIssues(t, issues)
			continue
		}
		diskOneIssue(t, issues, "$.spec.hardware.dataDisks[0].wwn")
	}
}

func TestADataDiskSizeIsExactAlignedAndAtMost64TiB(t *testing.T) {
	for name, test := range map[string]struct {
		disk     api.Value
		admitted bool
	}{
		"aligned":         {dataDisk("5000000000000001", "DATA-01", "metadataClear"), true},
		"exactly 64 TiB":  {dataDisk("5000000000000001", "DATA-01", "metadataClear").With("sizeBytes", api.IntegerValue("70368744177664")), true},
		"unaligned":       {dataDisk("5000000000000001", "DATA-01", "metadataClear").With("sizeBytes", api.IntegerValue("107374182401")), false},
		"past 64 TiB":     {dataDisk("5000000000000001", "DATA-01", "metadataClear").With("sizeBytes", api.IntegerValue("70368744181760")), false},
		"zero":            {dataDisk("5000000000000001", "DATA-01", "metadataClear").With("sizeBytes", api.IntegerValue("0")), false},
		"no sizeBytes":    {dataDisk("5000000000000001", "DATA-01", "metadataClear").Without("sizeBytes"), false},
		"a string size":   {dataDisk("5000000000000001", "DATA-01", "metadataClear").With("sizeBytes", api.StringValue("107374182400")), false},
		"a cleared disk":  {clearedDisk("5000000000000001", "DATA-01").With("sizeBytes", api.IntegerValue("107374182401")), false},
		"a cleared bound": {clearedDisk("5000000000000001", "DATA-01").With("sizeBytes", api.IntegerValue(diskOverBound)), true},
	} {
		t.Run(name, func(t *testing.T) {
			issues := physicalDisks(diskMachine(diskRoot(""), test.disk))
			if test.admitted {
				diskNoIssues(t, issues)
				return
			}
			diskOneIssue(t, issues, "$.spec.hardware.dataDisks[0].sizeBytes")
		})
	}
}

func TestDataDiskSerialNumbersAndWWNsAreDistinct(t *testing.T) {
	serial := diskMachine(diskRoot(""), clearedDisk("5000000000000001", "DATA-01"), clearedDisk("5000000000000002", "DATA-01"))
	diskOneIssue(t, physicalDisks(serial), "$.spec.hardware.dataDisks[1].serialNumber")
	wwn := diskMachine(diskRoot(""), clearedDisk("5000000000000001", "DATA-01"), clearedDisk("5000000000000001", "DATA-02"))
	diskOneIssue(t, physicalDisks(wwn), "$.spec.hardware.dataDisks[1].wwn")
	bySerial := diskMachine(diskRoot(""), clearedDisk("", "DATA-01"), clearedDisk("", "DATA-01"))
	diskOneIssue(t, physicalDisks(bySerial), "$.spec.hardware.dataDisks[1].serialNumber")
	diskNoIssues(t, physicalDisks(diskMachine(diskRoot(""), clearedDisk("5000000000000001", "DATA-01"), clearedDisk("5000000000000002", "DATA-02"), clearedDisk("", "DATA-03"), clearedDisk("", "DATA-04"))))
}

func TestADataDiskCountIsOneToFifteen(t *testing.T) {
	if MaxDataDisks != 15 {
		t.Fatalf("MaxDataDisks = %d", MaxDataDisks)
	}
	disks := func(count int) []api.Value {
		var disks []api.Value
		for index := range count {
			disks = append(disks, clearedDisk(fmt.Sprintf("5000%012x", index+1), fmt.Sprintf("SYNTHETIC-DATA-%02d", index)))
		}
		return disks
	}
	diskNoIssues(t, physicalDisks(diskMachine(diskRoot(""), disks(1)...)))
	diskNoIssues(t, physicalDisks(diskMachine(diskRoot(""), disks(15)...)))
	diskOneIssue(t, physicalDisks(diskMachine(diskRoot(""), disks(16)...)), "$.spec.hardware.dataDisks")
	empty := diskSet(diskMachine(diskRoot("")), list(), "hardware", "dataDisks")
	diskOneIssue(t, physicalDisks(empty), "$.spec.hardware.dataDisks")
}

func TestADataDiskEntryDeclaringNoActionRefusesAtThatEntry(t *testing.T) {
	retained := dataDisk("5000000000000002", "DATA-02", "retain").With("onInstall", api.StringValue("retain"))
	for name, disk := range map[string]api.Value{
		"no action":         dataDisk("5000000000000002", "DATA-02", ""),
		"onRemoval retain":  dataDisk("5000000000000002", "DATA-02", "retain"),
		"both retain":       retained,
		"onInstall retain":  dataDisk("5000000000000002", "DATA-02", "").With("onInstall", api.StringValue("retain")),
		"no action, no wwn": dataDisk("", "DATA-02", ""),
	} {
		t.Run(name, func(t *testing.T) {
			machine := diskMachine(diskRoot(""), clearedDisk("5000000000000001", "DATA-01"), disk)
			issue := diskOneIssue(t, physicalDisks(machine), "$.spec.hardware.dataDisks[1]")
			if !strings.Contains(issue.Message, "declares no action") || !strings.Contains(issue.Message, "DATA-02") {
				t.Fatalf("issue = %+v", issue)
			}
		})
	}
	diskNoIssues(t, physicalDisks(diskMachine(diskRoot(""), dataDisk("5000000000000002", "DATA-02", "metadataClear"))))
	diskNoIssues(t, physicalDisks(diskMachine(diskRoot(""), clearedDisk("5000000000000002", "DATA-02"))))
	diskNoIssues(t, physicalDisks(diskMachine(diskRoot(""), clearedDisk("5000000000000002", "DATA-02").With("onRemoval", api.StringValue("logicalOverwrite")))))
}

func TestADataDiskEntryNamingTheRootDiskRefusesAtThatEntry(t *testing.T) {
	hintedRoot := diskRoot("").Without("wwn").With("hints", m("deviceName", "/dev/disk/by-id/wwn-0x6000000000000001"))
	for name, test := range map[string]struct {
		root api.Value
		disk api.Value
	}{
		"the root serial number":                {diskRoot(""), clearedDisk("5000000000000001", diskRootSerial)},
		"the root serial number without a wwn":  {diskRoot(""), clearedDisk("", diskRootSerial)},
		"the root wwn":                          {diskRoot(""), clearedDisk(strings.TrimPrefix(diskRootWWN, "0x"), "DATA-01")},
		"the root wwn in another case":          {diskRoot(""), clearedDisk("", "DATA-01").With("wwn", api.StringValue("0X"+strings.ToUpper(strings.TrimPrefix(diskRootWWN, "0x"))))},
		"the root deviceName hint":              {hintedRoot, clearedDisk("6000000000000001", "DATA-01")},
		"a root named by serial only":           {m("serialNumber", diskRootSerial).With("sizeBytes", api.IntegerValue(diskRootSize)), clearedDisk("5000000000000001", diskRootSerial)},
		"a root named by wwn only":              {m("wwn", diskRootWWN), clearedDisk(strings.TrimPrefix(diskRootWWN, "0x"), "DATA-01")},
		"a root with a retained onRemoval":      {diskRoot("retain"), clearedDisk("5000000000000001", diskRootSerial)},
		"an erased root and an erasing entry":   {diskRoot("metadataClear"), dataDisk("5000000000000001", diskRootSerial, "metadataClear")},
		"an erased root and the same wwn entry": {diskRoot("metadataClear"), dataDisk(strings.TrimPrefix(diskRootWWN, "0x"), "DATA-01", "metadataClear")},
	} {
		t.Run(name, func(t *testing.T) {
			machine := diskMachine(test.root, clearedDisk("5000000000000009", "DATA-09"), test.disk)
			issue := diskIssueAt(t, physicalDisks(machine), "$.spec.hardware.dataDisks[1]")
			if !strings.Contains(issue.Message, "must not be the root disk") {
				t.Fatalf("issue = %+v", issue)
			}
			if !strings.Contains(issue.Remediation, "spec.hardware.rootDisk") {
				t.Fatalf("remediation = %q", issue.Remediation)
			}
			for _, other := range physicalDisks(machine) {
				if strings.HasPrefix(other.Field, "$.spec.hardware.dataDisks[0]") {
					t.Fatalf("the unrelated entry was refused: %+v", other)
				}
			}
		})
	}
	diskNoIssues(t, physicalDisks(diskMachine(diskRoot(""), clearedDisk("5000000000000001", "DATA-01"))))
}

func TestAnErasureTargetsAreDerivedFromTheRootAndTheDataDisks(t *testing.T) {
	const rootPath = "/dev/disk/by-id/wwn-" + diskRootWWN
	machine := diskMachine(diskRoot("logicalOverwrite"),
		dataDisk("5000000000000001", "DATA-01", "metadataClear"),
		clearedDisk("5000000000000002", "DATA-02"),
		dataDisk("", "DATA-03", "logicalOverwrite").With("onInstall", api.StringValue("metadataClear")),
		dataDisk("5000000000000004", "DATA-04", "retain").With("onInstall", api.StringValue("metadataClear")))
	const dataSize, rootSize = int64(107374182400), int64(480103981056)
	wantErase := []substrate.DiskTarget{
		{DeviceName: rootPath, WWN: diskRootWWN, SerialNumber: diskRootSerial, SizeBytes: rootSize, Method: "logicalOverwrite"},
		{DeviceName: "/dev/disk/by-id/wwn-0x5000000000000001", WWN: "0x5000000000000001", SerialNumber: "DATA-01", SizeBytes: dataSize, Method: "metadataClear"},
		{SerialNumber: "DATA-03", SizeBytes: dataSize, Method: "logicalOverwrite"},
	}
	if got := substrate.EraseTargets(machine); !slices.Equal(got, wantErase) {
		t.Fatalf("erase targets = %+v, want %+v", got, wantErase)
	}
	wantClear := []substrate.DiskTarget{
		{DeviceName: "/dev/disk/by-id/wwn-0x5000000000000002", WWN: "0x5000000000000002", SerialNumber: "DATA-02", SizeBytes: dataSize},
		{SerialNumber: "DATA-03", SizeBytes: dataSize},
		{DeviceName: "/dev/disk/by-id/wwn-0x5000000000000004", WWN: "0x5000000000000004", SerialNumber: "DATA-04", SizeBytes: dataSize},
	}
	if got := substrate.ClearTargets(machine); !slices.Equal(got, wantClear) {
		t.Fatalf("clear targets = %+v, want %+v", got, wantClear)
	}
	if !RootEraseSelected(machine) {
		t.Fatal("a root with a non-retain onRemoval was not selected")
	}
	if size, byDevice, found := substrate.SerialEraseTarget(machine, "DATA-03"); !found || byDevice || size != dataSize {
		t.Fatalf("serial erase target = %d %v %v", size, byDevice, found)
	}
	if _, _, found := substrate.SerialEraseTarget(machine, "DATA-02"); found {
		t.Fatal("a clear-only disk was an erase target")
	}
}

func TestTheRootIsAnErasureTargetOnlyWhenItsOnRemovalErases(t *testing.T) {
	volume := m("storageId", "s", "volumeId", "v")
	for name, root := range map[string]api.Value{
		"wwn":               m("wwn", diskRootWWN),
		"serial and size":   m("serialNumber", diskRootSerial).With("sizeBytes", api.IntegerValue(diskRootSize)),
		"volumeRef":         m("volumeRef", volume),
		"bootVolume":        m("bootVolume", m()),
		"explicit retain":   diskRoot("retain"),
		"an empty rootDisk": m(),
	} {
		t.Run(name, func(t *testing.T) {
			machine := diskMachine(root, dataDisk("5000000000000001", "DATA-01", "metadataClear"))
			diskNoIssues(t, physicalDisks(machine))
			if RootEraseSelected(machine) {
				t.Fatal("a root without an erasing onRemoval was selected")
			}
			targets := substrate.EraseTargets(machine)
			if len(targets) != 1 || targets[0].SerialNumber != "DATA-01" {
				t.Fatalf("erase targets = %+v", targets)
			}
		})
	}
	for _, method := range []string{"metadataClear", "logicalOverwrite"} {
		machine := diskMachine(diskRoot(method))
		if !RootEraseSelected(machine) {
			t.Fatalf("a root erased by %s was not selected", method)
		}
		if targets := substrate.EraseTargets(machine); len(targets) != 1 || targets[0].Method != method {
			t.Fatalf("erase targets = %+v", targets)
		}
	}
}

func TestADataDiskSerialDeclaredByAnotherMachineRefuses(t *testing.T) {
	other := func(spec api.Value) api.Object { return object(api.Machine, "other", spec) }
	declaresData := other(m("hardware", m("dataDisks", list(clearedDisk("5000000000000009", "SHARED-01")))))
	declaresRoot := other(m("hardware", m("rootDisk", m("serialNumber", "SHARED-01").With("sizeBytes", api.IntegerValue(diskRootSize)))))
	unrelated := other(m("hardware", m("dataDisks", list(clearedDisk("5000000000000009", "OTHER-01")))))
	for name, test := range map[string]struct {
		neighbor api.Object
		disk     api.Value
		refused  bool
	}{
		"a data disk by serial against a data disk": {declaresData, clearedDisk("", "SHARED-01"), true},
		"a data disk by serial against a root":      {declaresRoot, clearedDisk("", "SHARED-01"), true},
		"a data disk with a wwn":                    {declaresData, clearedDisk("5000000000000001", "SHARED-01"), false},
		"a distinct serial":                         {unrelated, clearedDisk("", "SHARED-01"), false},
	} {
		t.Run(name, func(t *testing.T) {
			machine := diskMachine(diskRoot(""), clearedDisk("5000000000000002", "DATA-02"), test.disk)
			issues := validateSelectingSerials(machine, api.NewCatalog([]api.Object{machine, test.neighbor}))
			if !test.refused {
				diskNoIssues(t, issues)
				return
			}
			issue := diskOneIssue(t, issues, "$.spec.hardware.dataDisks[1].serialNumber")
			if !strings.Contains(issue.Message, test.neighbor.Identity()) || !strings.Contains(issue.Remediation, "data disk's wwn") {
				t.Fatalf("issue = %+v", issue)
			}
		})
	}
	machine := diskMachine(diskRoot(""), clearedDisk("", "SHARED-01"))
	diskNoIssues(t, validateSelectingSerials(machine, api.NewCatalog([]api.Object{machine})))
}
