package baremetal

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const provedDigest = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func proof(t *testing.T, uuid, serial string) []byte {
	t.Helper()
	return encode(t, Evidence{
		Addresses: fixtureRequest(t).Addresses(), Postcondition: true, Power: "Off",
		Request: provedDigest, Serial: serial, UUID: uuid,
	})
}

func refusedAs(t *testing.T, err error) diagnostics.Diagnostic {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) == 0 {
		t.Fatalf("error carries no diagnostic: %v", err)
	}
	return reported[0]
}

// Only a proof that the machine is present, with an identity, carries one.
// The request digest is not compared: whoever reads a pin reads it outside the
// attempt that proved it, and the proof was bound to its request when it was
// accepted.
func TestTheProvedIdentityIsReadOnlyFromAPresenceProof(t *testing.T) {
	for name, test := range map[string]struct {
		evidence []byte
		want     machineref.HardwareIdentity
	}{
		"uuid and serial": {proof(t, "uuid-1", "SN1"), machineref.HardwareIdentity{UUID: "uuid-1", Serial: "SN1"}},
		"uuid alone":      {proof(t, "uuid-1", ""), machineref.HardwareIdentity{UUID: "uuid-1"}},
		"serial alone":    {proof(t, "", "SN1"), machineref.HardwareIdentity{Serial: "SN1"}},
	} {
		t.Run(name, func(t *testing.T) {
			identity, err := ProvedIdentity(test.evidence)
			if err != nil {
				t.Fatal(err)
			}
			if identity != test.want {
				t.Fatalf("identity = %+v, want %+v", identity, test.want)
			}
		})
	}
	valid := string(proof(t, "uuid-1", "SN1"))
	for name, evidence := range map[string][]byte{
		"null":            []byte("null"),
		"the absent form": encode(t, Evidence{Absent: true, Addresses: []string{}, Postcondition: true, Request: provedDigest}),
		"absent naming an identity": encode(t, Evidence{
			Absent: true, Addresses: fixtureRequest(t).Addresses(), Postcondition: true, Power: "Off",
			Request: provedDigest, Serial: "SN1", UUID: "uuid-1",
		}),
		"unproved":          encode(t, Evidence{Addresses: fixtureRequest(t).Addresses(), Power: "Off", Request: provedDigest, UUID: "uuid-1"}),
		"no identity":       proof(t, "", ""),
		"an unknown field":  []byte(strings.TrimSuffix(valid, "}") + `,"unexpected":true}`),
		"a trailing object": []byte(valid + "{}"),
		"trailing bytes":    []byte(valid + "x"),
		"a closing brace":   []byte(valid + "}"),
		"a closing bracket": []byte(valid + "]"),
		"empty":             nil,
		"beyond the bound": encode(t, Evidence{
			Addresses: fixtureRequest(t).Addresses(), Model: strings.Repeat("m", maxEvidenceBytes), Postcondition: true,
			Power: "Off", Request: provedDigest, UUID: "uuid-1",
		}),
	} {
		t.Run(name, func(t *testing.T) {
			identity, err := ProvedIdentity(evidence)
			if err == nil {
				t.Fatalf("evidence %q proved the identity %+v", evidence, identity)
			}
			if code := refusedAs(t, err).Code; code != "lifecycle.state" {
				t.Fatalf("code = %q", code)
			}
			if identity.Present() {
				t.Fatalf("a refusal returned the identity %+v", identity)
			}
		})
	}
}

func published(implementation string, verb reconciliation.Verb, state reconciliation.BlockState, evidence json.RawMessage) lifecycle.BlockEvidence {
	return lifecycle.BlockEvidence{
		Kind: Kind, Object: "server", Implementation: implementation, Verb: verb, State: state, Evidence: evidence,
	}
}

// A Machine is pinned by the evidence its bare-metal block recorded while the
// context's current apply proved it, and by nothing else: another
// implementation's block, a removal, a block that is not done, and a done
// block that carries no evidence (stored as JSON null) pin nothing.
func TestOnlyADoneBareMetalApplyProofPinsAMachine(t *testing.T) {
	proved := proof(t, "uuid-1", "SN1")
	installed := json.RawMessage(`{"hostKey":"ssh-ed25519 AAAA"}`)
	for name, blocks := range map[string][]lifecycle.BlockEvidence{
		"the proof alone": {published(Implementation, reconciliation.Apply, reconciliation.BlockDone, proved)},
		"beside the installation": {
			published("os-install-anaconda-v1", reconciliation.Apply, reconciliation.BlockDone, installed),
			published(Implementation, reconciliation.Apply, reconciliation.BlockDone, proved),
		},
	} {
		t.Run(name, func(t *testing.T) {
			identity, found, err := PinnedIdentity("server", blocks)
			if err != nil || !found {
				t.Fatalf("pin = %+v, %t, %v", identity, found, err)
			}
			if identity != (machineref.HardwareIdentity{UUID: "uuid-1", Serial: "SN1"}) {
				t.Fatalf("identity = %+v", identity)
			}
		})
	}
	for name, blocks := range map[string][]lifecycle.BlockEvidence{
		"no block":                  nil,
		"the libvirt machine":       {published("machine-libvirt-v1", reconciliation.Apply, reconciliation.BlockDone, proved)},
		"the installation alone":    {published("os-install-anaconda-v1", reconciliation.Apply, reconciliation.BlockDone, installed)},
		"a removal holding a proof": {published(Implementation, reconciliation.Destroy, reconciliation.BlockDone, proved)},
		"a completed removal": {published(Implementation, reconciliation.Destroy, reconciliation.BlockDone,
			encode(t, Evidence{Absent: true, Addresses: []string{}, Postcondition: true, Request: provedDigest}))},
		"pending":          {published(Implementation, reconciliation.Apply, reconciliation.BlockPending, nil)},
		"running":          {published(Implementation, reconciliation.Apply, reconciliation.BlockRunning, proved)},
		"failed":           {published(Implementation, reconciliation.Apply, reconciliation.BlockFailed, proved)},
		"unknown":          {published(Implementation, reconciliation.Apply, reconciliation.BlockUnknown, proved)},
		"no evidence":      {published(Implementation, reconciliation.Apply, reconciliation.BlockDone, nil)},
		"resolved as null": {published(Implementation, reconciliation.Apply, reconciliation.BlockDone, json.RawMessage("null"))},
	} {
		t.Run(name, func(t *testing.T) {
			identity, found, err := PinnedIdentity("server", blocks)
			if err != nil || found || identity.Present() {
				t.Fatalf("pin = %+v, %t, %v", identity, found, err)
			}
		})
	}
	for name, evidence := range map[string]json.RawMessage{
		"the absent form": encode(t, Evidence{Absent: true, Addresses: []string{}, Postcondition: true, Request: provedDigest}),
		"absent naming an identity": encode(t, Evidence{
			Absent: true, Addresses: []string{}, Postcondition: true, Request: provedDigest, Serial: "SN1", UUID: "uuid-1",
		}),
		"malformed": json.RawMessage(`{"uuid":`),
	} {
		t.Run(name, func(t *testing.T) {
			identity, found, err := PinnedIdentity("server", []lifecycle.BlockEvidence{
				published(Implementation, reconciliation.Apply, reconciliation.BlockDone, evidence),
			})
			if err == nil || found || identity.Present() {
				t.Fatalf("pin = %+v, %t, %v", identity, found, err)
			}
			reported := refusedAs(t, err)
			if reported.Code != "lifecycle.state" || !strings.Contains(reported.Message, "Machine/server") {
				t.Fatalf("refusal = %+v", reported)
			}
			if reported.Remediation != "destroy and apply this context so the machine is proved again" {
				t.Fatalf("remediation = %q", reported.Remediation)
			}
		})
	}
}

// Each character a reported identity may not hold: a C0 control, DEL, a C1
// control, and three Unicode format characters that print as nothing.
var unprintableCharacters = map[string]string{
	"a C0 control":       "\x1b",
	"DEL":                "\x7f",
	"a C1 control":       "\u009b",
	"a bidi override":    "\u202e",
	"a zero-width space": "\u200b",
	"a byte-order mark":  "\ufeff",
}

// A reported UUID or serial is proved only printable and at most 128
// characters once its surrounding space is trimmed, because a pin carries it
// into every later comparison and refusal. The presence proof names the field
// and the controller, the reading of a recorded proof names the field, and a
// pin read from a record that breaks the rule names the Machine and the field;
// none of them names the value.
func TestAReportedIdentityIsPrintableAndBounded(t *testing.T) {
	request := fixtureRequest(t)
	endpoint := request.Controller.Endpoint
	const unprintable, unbounded = " holding a character that is not printable", " longer than 128 characters"
	type row struct{ uuid, serial, fault string }
	refused := map[string]row{
		"a UUID of 129 characters":         {strings.Repeat("u", 129), "SN1", "a UUID" + unbounded},
		"a serial of 129 characters":       {"uuid-1", strings.Repeat("s", 129), "a SerialNumber" + unbounded},
		"a serial of 129 two-byte letters": {"uuid-1", strings.Repeat("\u00e9", 129), "a SerialNumber" + unbounded},
		"a tab inside a serial":            {"uuid-1", "SN\t1", "a SerialNumber" + unprintable},
	}
	for name, character := range unprintableCharacters {
		refused["a UUID holding "+name] = row{"uuid-" + character + "1", "SN1", "a UUID" + unprintable}
		refused["a serial holding "+name] = row{"uuid-1", "SN" + character + "1", "a SerialNumber" + unprintable}
	}
	for name, test := range refused {
		t.Run(name, func(t *testing.T) {
			evidence := Evidence{
				Addresses: request.Addresses(), Postcondition: true, Power: "Off", Request: provedDigest,
				Serial: test.serial, UUID: test.uuid,
			}
			value := test.uuid
			if strings.Contains(test.fault, "SerialNumber") {
				value = test.serial
			}
			err := ValidatePresence(encode(t, evidence), request, provedDigest)
			if err == nil {
				t.Fatal("the proof accepted an identity the evidence may not carry")
			}
			assertRefusal(t, err, "the management controller at "+endpoint+" reported "+test.fault, "", value)
			identity, err := ProvedIdentity(encode(t, evidence))
			if err == nil || identity.Present() {
				t.Fatalf("the recorded proof yielded %+v, %v", identity, err)
			}
			assertRefusal(t, err, "the machine evidence records "+test.fault, "", value)
			identity, found, err := PinnedIdentity("server", []lifecycle.BlockEvidence{
				published(Implementation, reconciliation.Apply, reconciliation.BlockDone, encode(t, evidence)),
			})
			if err == nil || found || identity.Present() {
				t.Fatalf("pin = %+v, %t, %v", identity, found, err)
			}
			assertRefusal(t, err, "the proof that pins Machine/server records "+test.fault, reprove, value)
		})
	}
	for name, test := range map[string]struct{ uuid, serial string }{
		"a serial with inner spaces":          {"uuid-1", "SN 12 34"},
		"128 characters each":                 {strings.Repeat("u", 128), strings.Repeat("s", 128)},
		"128 two-byte letters":                {"uuid-1", strings.Repeat("\u00e9", 128)},
		"128 characters in surrounding space": {" \t" + strings.Repeat("u", 128) + "\n ", " SN1\u00a0"},
	} {
		t.Run(name, func(t *testing.T) {
			evidence := encode(t, Evidence{
				Addresses: request.Addresses(), Postcondition: true, Power: "Off", Request: provedDigest,
				Serial: test.serial, UUID: test.uuid,
			})
			if err := ValidatePresence(evidence, request, provedDigest); err != nil {
				t.Fatalf("a printable identity was refused: %v", err)
			}
			want := machineref.HardwareIdentity{UUID: strings.TrimSpace(test.uuid), Serial: strings.TrimSpace(test.serial)}
			identity, found, err := PinnedIdentity("server", []lifecycle.BlockEvidence{
				published(Implementation, reconciliation.Apply, reconciliation.BlockDone, evidence),
			})
			if err != nil || !found || identity != want {
				t.Fatalf("pin = %+v, %t, %v; want %+v", identity, found, err, want)
			}
		})
	}
}

func assertRefusal(t *testing.T, err error, message, remediation, value string) {
	t.Helper()
	reported := refusedAs(t, err)
	if reported.Code != "lifecycle.state" || reported.Message != message || reported.Remediation != remediation {
		t.Fatalf("refusal = %+v, want %q remedied by %q", reported, message, remediation)
	}
	if strings.Contains(reported.Message, value) || strings.Contains(reported.Remediation, value) {
		t.Fatalf("the refusal %+v names the reported value", reported)
	}
}
