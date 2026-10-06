package trust

import (
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func record(t *testing.T, machine, address string, seed byte) Record {
	t.Helper()
	host := key(t, seed)
	return Record{
		Machine: machine, Address: address, Port: 22,
		KeyType: host.Type, PublicKey: host.PublicKey, Fingerprint: host.Fingerprint(),
		Source: SourceEnrollment, Recorded: "2026-09-16T00:00:00Z",
	}
}

func TestAnAbsentTrustStoreReadsAsEmptyRatherThanDamaged(t *testing.T) {
	for _, data := range [][]byte{nil, {}, []byte("  \n ")} {
		store, err := Decode(data)
		if err != nil {
			t.Fatalf("%q: %v", data, err)
		}
		if store.FormatVersion != FormatVersion || len(store.Hosts) != 0 {
			t.Fatalf("%q decoded to %+v", data, store)
		}
	}
}

func TestTrustRecordsRoundTripInMachineOrder(t *testing.T) {
	store := Store{FormatVersion: FormatVersion}
	store.Upsert(record(t, "node-b", "192.0.2.11", 2))
	store.Upsert(record(t, "node-a", "192.0.2.10", 1))
	data, err := store.Encode()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Hosts) != 2 || decoded.Hosts[0].Machine != "node-a" || decoded.Hosts[1].Machine != "node-b" {
		t.Fatalf("decoded = %+v", decoded.Hosts)
	}
	again, err := decoded.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(data) {
		t.Fatalf("re-encoded differs:\n%s\n%s", data, again)
	}
}

func TestUpsertReplacesOneMachineWithoutDisturbingAnother(t *testing.T) {
	store := Store{FormatVersion: FormatVersion}
	store.Upsert(record(t, "node-a", "192.0.2.10", 1))
	store.Upsert(record(t, "node-b", "192.0.2.11", 2))
	rotated := record(t, "node-a", "192.0.2.10", 3)
	store.Upsert(rotated)
	if len(store.Hosts) != 2 {
		t.Fatalf("hosts = %+v", store.Hosts)
	}
	found, ok := store.Find("node-a")
	if !ok || found.PublicKey != rotated.PublicKey {
		t.Fatalf("node-a = %+v", found)
	}
	if _, ok := store.Find("absent"); ok {
		t.Fatal("an unrecorded Machine was found")
	}
}

// OpenSSH accepts any entry matching a host, so two records pinning one address
// to different keys would let a peer present either and still pass.
func TestOneAddressPinsExactlyOneKey(t *testing.T) {
	store := Store{FormatVersion: FormatVersion}
	store.Upsert(record(t, "node-a", "192.0.2.10", 1))
	store.Upsert(record(t, "node-b", "192.0.2.10", 2))
	err := store.Validate()
	if err == nil {
		t.Fatal("divergent keys for one address were accepted")
	}
	if got := code(t, err); got != "trust.identity" {
		t.Fatalf("code = %q", got)
	}
	if _, encodeErr := store.Encode(); encodeErr == nil {
		t.Fatal("encoding published a store no session could read")
	}
	shared := record(t, "node-b", "192.0.2.10", 1)
	store.Upsert(shared)
	if err := store.Validate(); err != nil {
		t.Fatalf("two Machines at one address with the same key: %v", err)
	}
}

func TestATrustStoreRefusesContentNoSessionCouldRead(t *testing.T) {
	valid := record(t, "node-a", "192.0.2.10", 1)
	for _, test := range []struct{ name, data string }{
		{"unknown field", `{"formatVersion":1,"hosts":[],"extra":true}`},
		{"another version", `{"formatVersion":2,"hosts":[]}`},
		{"trailing content", `{"formatVersion":1,"hosts":[]}{}`},
		{"a closing brace", `{"formatVersion":1,"hosts":[]}}`},
		{"a closing bracket", `{"formatVersion":1,"hosts":[]}]`},
		{"malformed", `{"formatVersion":1,`},
		{"unqualified key type", `{"formatVersion":1,"hosts":[{"machine":"a","address":"192.0.2.10","port":22,"keyType":"ssh-dss","publicKey":"` + valid.PublicKey + `"}]}`},
		{"no machine", `{"formatVersion":1,"hosts":[{"machine":"","address":"192.0.2.10","port":22,"keyType":"` + valid.KeyType + `","publicKey":"` + valid.PublicKey + `"}]}`},
		{"duplicate machine", `{"formatVersion":1,"hosts":[{"machine":"a","address":"192.0.2.10","port":22,"keyType":"` + valid.KeyType + `","publicKey":"` + valid.PublicKey + `"},{"machine":"a","address":"192.0.2.11","port":22,"keyType":"` + valid.KeyType + `","publicKey":"` + valid.PublicKey + `"}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if store, err := Decode([]byte(test.data)); err == nil {
				t.Fatalf("accepted %s as %+v", test.data, store)
			}
		})
	}
}

func TestATrustStoreIsBoundedInRecordsAndBytes(t *testing.T) {
	store := Store{FormatVersion: FormatVersion}
	base := record(t, "node", "192.0.2.10", 1)
	for i := range MaxHosts + 1 {
		entry := base
		entry.Machine = "node-" + strings.Repeat("a", i%3) + string(rune('a'+i%26)) + strings.Repeat("b", i/26)
		entry.Address = "192.0.2." + string(rune('0'+i%10))
		store.Hosts = append(store.Hosts, entry)
	}
	if err := store.Validate(); err == nil {
		t.Fatal("an unbounded store was accepted")
	}
}

func TestARecordCarriesTheKeyASessionPins(t *testing.T) {
	entry := record(t, "node-a", "192.0.2.10", 1)
	host := entry.HostKey()
	if !host.Present() || host.Fingerprint() != entry.Fingerprint {
		t.Fatalf("record key = %+v", host)
	}
}

// An address the context reassigned belongs to the Machine that declares it
// now, so a record of a Machine it no longer declares at that exact endpoint
// is removed with the write that takes it over, whatever key it pinned. A
// Machine still declared keeps its record, and a record at another address or
// port is another endpoint.
func TestARecordTakingOverAnUndeclaredEndpointRemovesIt(t *testing.T) {
	retired := record(t, "retired", "192.0.2.10", 2)
	kept := record(t, "elsewhere", "192.0.2.11", 3)
	otherPort := record(t, "ported", "192.0.2.10", 4)
	otherPort.Port = 2222
	store := Store{FormatVersion: FormatVersion, Hosts: []Record{otherPort, retired, kept}}
	declared := func(name string) bool { return name == "node-a" || name == "node-b" }
	removed := store.Supersede(record(t, "node-a", "192.0.2.10", 1), declared)
	if len(removed) != 1 || removed[0].Machine != "retired" || removed[0].PublicKey != retired.PublicKey {
		t.Fatalf("removed = %+v", removed)
	}
	if _, found := store.Find("retired"); found {
		t.Fatal("the undeclared record at the endpoint was kept")
	}
	for _, name := range []string{"node-a", "elsewhere", "ported"} {
		if _, found := store.Find(name); !found {
			t.Fatalf("%s was removed: %+v", name, store.Hosts)
		}
	}
	if err := store.Validate(); err != nil {
		t.Fatalf("the superseded store does not validate: %v", err)
	}

	stillDeclared := Store{FormatVersion: FormatVersion, Hosts: []Record{record(t, "node-b", "192.0.2.10", 2)}}
	if removed := stillDeclared.Supersede(record(t, "node-a", "192.0.2.10", 1), declared); len(removed) != 0 {
		t.Fatalf("a declared Machine's record was removed: %+v", removed)
	}
	err := stillDeclared.Validate()
	var pin *DivergentPin
	if !errors.As(err, &pin) || pin.Endpoint != "192.0.2.10" {
		t.Fatalf("a declared Machine's divergent record did not refuse: %v", err)
	}
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "trust.identity" ||
		!strings.Contains(reported[0].Message, "node-a") || !strings.Contains(reported[0].Message, "node-b") {
		t.Fatalf("refusal = %+v", reported)
	}
	restated := diagnostics.Of(pin.Retrust("lab", func(name string) bool { return name == "node-a" }, nil))
	if len(restated) != 1 || restated[0].Remediation != "re-trust node-b with bootwright machine trust --context lab --machines node-b --replace node-b" {
		t.Fatalf("restated = %+v", restated)
	}
	exempt := diagnostics.Of(pin.Retrust("lab", func(name string) bool { return name == "node-a" }, func(name string) string {
		if name == "node-b" {
			return "declares an explicit knownHostsRef"
		}
		return ""
	}))
	if len(exempt) != 1 || exempt[0].Remediation != exemptRemedy("node-b") {
		t.Fatalf("restated for a Machine that no longer uses the store = %+v", exempt)
	}
}

func exemptRemedy(machine string) string {
	return "drop " + machine + " from the input with bootwright context update --name lab --input-dir <dir>, repeat this command, then restore " +
		machine + " the same way; this needs a context with no incomplete operation and an input in which no other object references " +
		machine + ", and after a completed apply the next apply no longer settles: it refuses the changed input until a destroy"
}

// Dropping a Machine from the input is no remedy while an operation is
// incomplete, which context update refuses, nor for a Machine another object
// references, whose input would not compile, and over a completed apply it
// publishes a changed input the next apply refuses until a destroy. The
// refusal says all of that rather than offer a step that dead-ends.
func TestAnExemptPinRemedyStatesWhatDroppingTheMachineNeedsAndCosts(t *testing.T) {
	store := Store{FormatVersion: FormatVersion, Hosts: []Record{record(t, "node-b", "192.0.2.10", 2), record(t, "node-a", "192.0.2.10", 1)}}
	var pin *DivergentPin
	if err := store.Validate(); !errors.As(err, &pin) {
		t.Fatalf("a divergent pin did not refuse: %v", err)
	}
	reported := diagnostics.Of(pin.Retrust("lab", func(name string) bool { return name == "node-a" }, func(name string) string {
		if name == "node-b" {
			return "host key comes from its installation evidence"
		}
		return ""
	}))
	want := "trusting node-a at 192.0.2.10 would pin it to a key that diverges from the one this context trusts there for node-b; " +
		"node-b no longer uses this context's SSH trust (host key comes from its installation evidence), so nothing reads that record, " +
		"but the store drops it only once node-b leaves the input"
	if len(reported) != 1 || reported[0].Code != "trust.identity" || reported[0].Message != want || reported[0].Remediation != exemptRemedy("node-b") {
		t.Fatalf("refusal = %+v", reported)
	}
}

// The controller Machine, the one reached locally, leaves the input only when
// the Environment names another local controller, which the controller binding
// refuses once an apply has bound the context. Its refusal names that dead end
// and the separate context it leaves, rather than an input edit that refuses.
func TestTheControllerMachinePinRemedyNamesTheBindingThatKeepsItInTheInput(t *testing.T) {
	store := Store{FormatVersion: FormatVersion, Hosts: []Record{record(t, "controller", "192.0.2.10", 2), record(t, "node-a", "192.0.2.10", 1)}}
	var pin *DivergentPin
	if err := store.Validate(); !errors.As(err, &pin) {
		t.Fatalf("a divergent pin did not refuse: %v", err)
	}
	reported := diagnostics.Of(pin.Retrust("lab", func(name string) bool { return name == "node-a" }, func(name string) string {
		if name == "controller" {
			return ExemptReachedLocally
		}
		return ""
	}))
	want := "trusting node-a at 192.0.2.10 would pin it to a key that diverges from the one this context trusts there for controller; " +
		"controller no longer uses this context's SSH trust (reached locally), so nothing reads that record, but the store drops it only once controller leaves the input"
	if len(reported) != 1 || reported[0].Code != "trust.identity" || reported[0].Message != want || reported[0].Remediation != controllerRemedy("controller") {
		t.Fatalf("refusal = %+v", reported)
	}
}

func controllerRemedy(machine string) string {
	return machine + " is the controller Machine, which leaves the input only when spec.controller.machineRef names another local Machine; " +
		"before an apply binds this context to " + machine + ", and with no incomplete operation, make that change with " +
		"bootwright context update --name lab --input-dir <dir>, repeat this command, then restore " + machine + " the same way; " +
		"once an apply has bound this context, context update refuses any input that changes the controller Machine, " +
		"so no input edit drops the record and only a separate context does"
}
