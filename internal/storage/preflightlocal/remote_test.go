package preflightlocal

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/storage/preflight"
)

type remoteCall struct {
	context, machine string
	argv             []string
}

type fakeRemote struct {
	calls   []remoteCall
	answers map[string][]byte
	failing map[string]bool
}

func (f *fakeRemote) Run(_ context.Context, contextName, machine string, argv []string, bound int) ([]byte, int, error) {
	f.calls = append(f.calls, remoteCall{contextName, machine, slices.Clone(argv)})
	key := strings.Join(argv, " ")
	if f.failing[key] {
		return nil, 0, errors.New("SECRET-OUTPUT /root/.ssh/id_rsa")
	}
	if data, found := f.answers[key]; found {
		if len(data) > bound {
			return nil, 0, errors.New("SECRET-OUTPUT over its bound")
		}
		return data, 0, nil
	}
	return nil, remoteMissing, nil
}

func (f *fakeRemote) argvs() []string {
	result := make([]string, 0, len(f.calls))
	for _, call := range f.calls {
		result = append(result, strings.Join(call.argv, " "))
	}
	return result
}

const (
	nprocCommand   = "/usr/bin/nproc"
	meminfoCommand = "/usr/bin/cat /proc/meminfo"
	findCommand    = "/usr/bin/find /etc/libvirt/qemu /run/libvirt/qemu -maxdepth 1 -type f -name *.xml -print"
	tailPrefix     = "/usr/bin/tail -v -n +1 --"
)

func statCommand(path string) string {
	return "/usr/bin/stat --file-system --format=%a %S %i -- " + path
}

func domainFile(name, uuid string, vcpu string) string {
	return "<domain><name>" + name + "</name><uuid>" + uuid + "</uuid><vcpu>" + vcpu + "</vcpu></domain>"
}

func tailOutput(paths []string, contents []string) []byte {
	var out strings.Builder
	for index, path := range paths {
		if index > 0 {
			out.WriteString("\n")
		}
		out.WriteString("==> " + path + " <==\n" + contents[index])
	}
	return []byte(out.String())
}

var (
	remotePaths    = []string{"/etc/libvirt/qemu/foreign.xml", "/etc/libvirt/qemu/owned.xml", "/run/libvirt/qemu/foreign.xml"}
	remoteContents = []string{domainFile("other", "u2", "2") + "\n", domainFile("owned", "u1", "4"), `<domstatus pid="7">` + domainFile("other", "u2", "3") + "</domstatus>\n"}
)

func completeRemote() *fakeRemote {
	return &fakeRemote{answers: map[string][]byte{
		nprocCommand:            []byte("8\n"),
		meminfoCommand:          []byte("MemTotal: 4096 kB\nMemAvailable: 2048 kB\n"),
		statCommand("/var/lib"): []byte("1000 4096 abcdef0123456789\n"),
		findCommand:             []byte(strings.Join(remotePaths, "\n") + "\n"),
		tailPrefix + " " + strings.Join(remotePaths, " "): tailOutput(remotePaths, remoteContents),
	}}
}

func remoteTarget() preflight.Target {
	return preflight.Target{Host: "host-1", Pool: "/var/lib/libvirt/images", Context: "lab", Allocations: []preflight.Allocation{{Machine: "guest", Host: "host-1", Domain: "owned", UUID: "u1"}}}
}

func TestRemoteObservationRunsFixedCommandsAndParsesEveryFact(t *testing.T) {
	remote := completeRemote()
	facts, err := (Observer{Remote: remote}).Observe(context.Background(), remoteTarget())
	if err != nil {
		t.Fatal(err)
	}
	if !facts.CPUKnown || !facts.MemoryKnown || !facts.DiskKnown || facts.Reason != "" {
		t.Fatalf("%+v", facts)
	}
	if want := (preflight.Quantities{CPU: 8 - 3, Memory: 2048 * 1024, Disk: 1000 * 4096}); facts.Available != want {
		t.Fatalf("%+v", facts.Available)
	}
	if facts.DiskIdentity != "ssh/host-1/abcdef0123456789" || len(facts.Reusable) != 0 || len(facts.Unproved) != 0 {
		t.Fatalf("%+v", facts)
	}
	want := []string{nprocCommand, meminfoCommand, statCommand("/var/lib/libvirt/images"), statCommand("/var/lib/libvirt"), statCommand("/var/lib"), findCommand, tailPrefix + " " + strings.Join(remotePaths, " ")}
	if got := remote.argvs(); !slices.Equal(got, want) {
		t.Fatalf("%q", got)
	}
	for _, call := range remote.calls {
		if call.context != "lab" || call.machine != "host-1" {
			t.Fatalf("%+v", call)
		}
	}
	if got := remote.calls[2].argv; len(got) != 5 || got[1] != "--file-system" || got[2] != "--format=%a %S %i" || got[3] != "--" || got[4] != "/var/lib/libvirt/images" {
		t.Fatalf("%q", got)
	}
}

func TestRemoteDiskIdentityIsStablePerFilesystemAndNeverLocal(t *testing.T) {
	first, second := remoteTarget(), remoteTarget()
	second.Pool = "/var/lib/other"
	a, _ := (Observer{Remote: completeRemote()}).Observe(context.Background(), first)
	b, _ := (Observer{Remote: completeRemote()}).Observe(context.Background(), second)
	if a.DiskIdentity == "" || a.DiskIdentity != b.DiskIdentity {
		t.Fatalf("%q %q", a.DiskIdentity, b.DiskIdentity)
	}
	if strings.HasPrefix(a.DiskIdentity, "{") {
		t.Fatal("remote identity resembles a local one")
	}
}

func TestRemoteObservationWithNoOwnedDomainsCountsEveryForeignCPU(t *testing.T) {
	target := remoteTarget()
	target.Allocations = []preflight.Allocation{{Machine: "guest", Host: "host-1", Domain: "owned", UUID: "u9"}}
	facts, _ := (Observer{Remote: completeRemote()}).Observe(context.Background(), target)
	if facts.Available.CPU != 8-3-4 {
		t.Fatalf("%+v", facts)
	}
}

func TestRemoteObservationWithNoDomainFilesReadsNoneAndCountsNoForeignCPU(t *testing.T) {
	remote := completeRemote()
	remote.answers[findCommand] = nil
	facts, _ := (Observer{Remote: remote}).Observe(context.Background(), remoteTarget())
	if !facts.CPUKnown || facts.Available.CPU != 8 || strings.Contains(strings.Join(remote.argvs(), "\n"), "tail") {
		t.Fatalf("%+v %q", facts, remote.argvs())
	}
}

func TestRemoteProbeFailureLeavesOnlyThatFactUnknown(t *testing.T) {
	for _, row := range []struct {
		name    string
		failing []string
		cpu     bool
		memory  bool
		disk    bool
		reason  string
	}{
		{"cpu", []string{nprocCommand}, false, true, true, remoteFactsFailure},
		{"memory", []string{meminfoCommand}, true, false, true, remoteFactsFailure},
		{"disk", []string{statCommand("/var/lib")}, true, true, false, remoteFactsFailure},
		{"find", []string{findCommand}, false, true, true, remoteInventoryFailure},
		{"tail", []string{tailPrefix + " " + strings.Join(remotePaths, " ")}, false, true, true, remoteInventoryFailure},
	} {
		t.Run(row.name, func(t *testing.T) {
			remote := completeRemote()
			remote.failing = map[string]bool{}
			for _, key := range row.failing {
				remote.failing[key] = true
			}
			facts, err := (Observer{Remote: remote}).Observe(context.Background(), remoteTarget())
			if err != nil || facts.CPUKnown != row.cpu || facts.MemoryKnown != row.memory || facts.DiskKnown != row.disk || facts.Reason != row.reason {
				t.Fatalf("%+v %v", facts, err)
			}
			if strings.Contains(facts.Reason, "SECRET") || strings.Contains(facts.Reason, "id_rsa") {
				t.Fatal(facts.Reason)
			}
		})
	}
}

func TestRemoteUnparsedAnswersLeaveTheirFactUnknown(t *testing.T) {
	for name, edit := range map[string]func(*fakeRemote){
		"nproc zero":      func(f *fakeRemote) { f.answers[nprocCommand] = []byte("0\n") },
		"nproc signed":    func(f *fakeRemote) { f.answers[nprocCommand] = []byte("+4\n") },
		"nproc unended":   func(f *fakeRemote) { f.answers[nprocCommand] = []byte("4") },
		"nproc two lines": func(f *fakeRemote) { f.answers[nprocCommand] = []byte("4\n4\n") },
		"meminfo":         func(f *fakeRemote) { f.answers[meminfoCommand] = []byte("MemFree: 1 kB\n") },
		"stat fields":     func(f *fakeRemote) { f.answers[statCommand("/var/lib")] = []byte("1000 4096\n") },
		"stat overflow":   func(f *fakeRemote) { f.answers[statCommand("/var/lib")] = []byte("9223372036854775807 4096 ab\n") },
		"stat zero size":  func(f *fakeRemote) { f.answers[statCommand("/var/lib")] = []byte("1 0 ab\n") },
		"stat id":         func(f *fakeRemote) { f.answers[statCommand("/var/lib")] = []byte("1 4096 xyz\n") },
		"stat negative":   func(f *fakeRemote) { f.answers[statCommand("/var/lib")] = []byte("-1 4096 ab\n") },
	} {
		t.Run(name, func(t *testing.T) {
			remote := completeRemote()
			edit(remote)
			facts, _ := (Observer{Remote: remote}).Observe(context.Background(), remoteTarget())
			known := facts.CPUKnown && facts.MemoryKnown && facts.DiskKnown
			if known || facts.Reason == "" {
				t.Fatalf("%+v", facts)
			}
		})
	}
}

func TestRemoteDomainInventoryRefusesAnythingItDidNotRequest(t *testing.T) {
	joined := strings.Join(remotePaths, " ")
	for name, edit := range map[string]func(*fakeRemote){
		"unexpected directory": func(f *fakeRemote) { f.answers[findCommand] = []byte("/tmp/x.xml\n") },
		"nested path":          func(f *fakeRemote) { f.answers[findCommand] = []byte("/etc/libvirt/qemu/a/b.xml\n") },
		"relative":             func(f *fakeRemote) { f.answers[findCommand] = []byte("etc/libvirt/qemu/a.xml\n") },
		"not xml":              func(f *fakeRemote) { f.answers[findCommand] = []byte("/etc/libvirt/qemu/a.txt\n") },
		"control byte":         func(f *fakeRemote) { f.answers[findCommand] = []byte("/etc/libvirt/qemu/a\x01.xml\n") },
		"empty name":           func(f *fakeRemote) { f.answers[findCommand] = []byte("/etc/libvirt/qemu/.xml\n") },
		"unterminated":         func(f *fakeRemote) { f.answers[findCommand] = []byte("/etc/libvirt/qemu/a.xml") },
		"duplicate": func(f *fakeRemote) {
			f.answers[findCommand] = []byte("/etc/libvirt/qemu/a.xml\n/etc/libvirt/qemu/a.xml\n")
		},
		"header mismatch": func(f *fakeRemote) {
			f.answers[tailPrefix+" "+joined] = []byte(strings.Replace(string(tailOutput(remotePaths, remoteContents)), "owned.xml <==", "other.xml <==", 1))
		},
		"extra header": func(f *fakeRemote) {
			f.answers[tailPrefix+" "+joined] = append(tailOutput(remotePaths, remoteContents), []byte("\n==> /etc/passwd <==\nroot\n")...)
		},
		"embedded header": func(f *fakeRemote) {
			f.answers[tailPrefix+" "+joined] = tailOutput(remotePaths, []string{remoteContents[0] + "==> /etc/shadow <==\n", remoteContents[1], remoteContents[2]})
		},
		"missing header": func(f *fakeRemote) { f.answers[tailPrefix+" "+joined] = []byte(remoteContents[0]) },
		"truncated": func(f *fakeRemote) {
			f.answers[tailPrefix+" "+joined] = tailOutput(remotePaths[:2], remoteContents[:2])
		},
		"not a domain": func(f *fakeRemote) {
			f.answers[tailPrefix+" "+joined] = tailOutput(remotePaths, []string{"<network/>\n", remoteContents[1], remoteContents[2]})
		},
		"identity contradicted": func(f *fakeRemote) {
			f.answers[tailPrefix+" "+joined] = tailOutput(remotePaths, []string{domainFile("renamed", "u2", "2") + "\n", remoteContents[1], remoteContents[2]})
		},
		"foreign holds ours": func(f *fakeRemote) {
			f.answers[tailPrefix+" "+joined] = tailOutput(remotePaths, []string{domainFile("impostor", "u1", "2") + "\n", domainFile("third", "u3", "1"), remoteContents[2]})
		},
	} {
		t.Run(name, func(t *testing.T) {
			remote := completeRemote()
			edit(remote)
			facts, err := (Observer{Remote: remote}).Observe(context.Background(), remoteTarget())
			if err != nil || facts.CPUKnown || !facts.MemoryKnown || !facts.DiskKnown || facts.Reason != remoteInventoryFailure {
				t.Fatalf("%+v %v", facts, err)
			}
		})
	}
}

func TestRemoteDomainListingIsBoundedPerDirectory(t *testing.T) {
	var listing strings.Builder
	for index := 0; index <= remoteEntryBound; index++ {
		listing.WriteString("/etc/libvirt/qemu/d" + strconv.Itoa(index) + ".xml\n")
	}
	if _, err := parseDomainListing([]byte(listing.String())); err == nil {
		t.Fatal("an over-bound inventory was admitted")
	}
}

func TestRemoteDiskWalkIsBoundedAndNeedsAnAbsoluteCleanPath(t *testing.T) {
	deep := strings.Repeat("/d", remotePathDepthBound+1)
	for _, pool := range []string{"", "relative", "/a/../b", "/a//b", "/a/b/", "/a\nb", deep} {
		remote := completeRemote()
		target := remoteTarget()
		target.Pool = pool
		facts, _ := (Observer{Remote: remote}).Observe(context.Background(), target)
		if facts.DiskKnown || slices.ContainsFunc(remote.argvs(), func(c string) bool { return strings.Contains(c, "/usr/bin/stat") }) {
			t.Fatalf("%q: %+v %q", pool, facts, remote.argvs())
		}
	}
	remote := completeRemote()
	delete(remote.answers, statCommand("/var/lib"))
	facts, _ := (Observer{Remote: remote}).Observe(context.Background(), remoteTarget())
	stats := 0
	for _, call := range remote.argvs() {
		if strings.Contains(call, "/usr/bin/stat") {
			stats++
		}
	}
	if facts.DiskKnown || stats != 5 || !strings.HasSuffix(remote.argvs()[2+stats-1], "-- /") {
		t.Fatalf("%+v %q", facts, remote.argvs())
	}
}

func TestRemoteAllocatedMachineIsUnprovedAndEarnsNoCredit(t *testing.T) {
	target := remoteTarget()
	target.Allocations[0].Allocated = true
	facts, err := (Observer{Remote: completeRemote()}).Observe(context.Background(), target)
	if err != nil || len(facts.Reusable) != 0 || facts.Unproved["guest"] != "remote reuse credit is not observed" || len(facts.Unproved) != 1 {
		t.Fatalf("%+v %v", facts, err)
	}
}

func TestRemoteObservationFailingEverywhereNamesTheHost(t *testing.T) {
	facts, err := (Observer{Remote: &fakeRemote{}}).Observe(context.Background(), remoteTarget())
	if err != nil || facts.CPUKnown || facts.MemoryKnown || facts.DiskKnown || !strings.Contains(facts.Reason, "host-1") || strings.Contains(facts.Reason, "SECRET") {
		t.Fatalf("%+v %v", facts, err)
	}
}

func TestRemoteObservationNeedsAnExplicitContextAndHost(t *testing.T) {
	for _, edit := range []func(*preflight.Target){func(t *preflight.Target) { t.Context = "" }, func(t *preflight.Target) { t.Host = "" }} {
		remote := completeRemote()
		target := remoteTarget()
		edit(&target)
		facts, _ := (Observer{Remote: remote}).Observe(context.Background(), target)
		if len(remote.calls) != 0 || facts.CPUKnown || facts.MemoryKnown || facts.DiskKnown || facts.Reason == "" {
			t.Fatalf("%+v %q", facts, remote.argvs())
		}
	}
}

func TestRemoteObservationStopsWhenTheContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	remote := completeRemote()
	if _, err := (Observer{Remote: remote}).Observe(ctx, remoteTarget()); !errors.Is(err, context.Canceled) || len(remote.calls) != 0 {
		t.Fatalf("%v %q", err, remote.argvs())
	}
}

// A pool stat that did not complete leaves the disk unknown: only an absent
// path sends the walk to its parent, so no other filesystem stands in for it.
func TestRemoteDiskWalkClimbsOnlyPastAnAbsentPath(t *testing.T) {
	remote := completeRemote()
	remote.failing = map[string]bool{statCommand("/var/lib/libvirt/images"): true}
	facts, _ := (Observer{Remote: remote}).Observe(context.Background(), remoteTarget())
	if facts.DiskKnown || slices.Contains(remote.argvs(), statCommand("/var/lib/libvirt")) {
		t.Fatalf("%+v %q", facts, remote.argvs())
	}
}

// A host with many defined and running domains is read whole: the domain files
// have their own bound, far above the bound of a single fact.
func TestRemoteDomainFilesAboveOneFactBoundStillCountForeignCPUs(t *testing.T) {
	remote := completeRemote()
	large := domainFile("other", "u2", "2") + strings.Repeat("<!-- status -->", remoteFactBound/8) + "\n"
	contents := slices.Clone(remoteContents)
	contents[0] = large
	remote.answers[tailPrefix+" "+strings.Join(remotePaths, " ")] = tailOutput(remotePaths, contents)
	facts, err := (Observer{Remote: remote}).Observe(context.Background(), remoteTarget())
	if err != nil || !facts.CPUKnown || facts.Available.CPU != 5 {
		t.Fatalf("%+v %v", facts, err)
	}
}

func TestADiskOnlyTargetRunsOnlyTheFilesystemRead(t *testing.T) {
	remote := &fakeRemote{answers: map[string][]byte{statCommand("/var/lib/bootwright-services"): []byte("10 4096 ab\n")}}
	facts, err := observeRemote(context.Background(), remote, preflight.Target{Host: "host", Context: "lab", Pool: "/var/lib/bootwright-services/lab/artifact-server/files/public/os/rhel-01/install.iso", DiskOnly: true})
	if err != nil || !facts.DiskKnown || facts.CPUKnown || facts.MemoryKnown || facts.Available.Disk != 40960 || facts.DiskIdentity != "ssh/host/ab" {
		t.Fatalf("%+v %v", facts, err)
	}
	for _, argv := range remote.argvs() {
		if !strings.HasPrefix(argv, "/usr/bin/stat --file-system") {
			t.Fatalf("a disk-only observation ran %q", argv)
		}
	}
	facts, err = observeRemote(context.Background(), &fakeRemote{failing: map[string]bool{statCommand("/pool"): true}}, preflight.Target{Host: "host", Context: "lab", Pool: "/pool", DiskOnly: true})
	if err != nil || facts.DiskKnown || facts.Reason == "" {
		t.Fatalf("%+v %v", facts, err)
	}
}
