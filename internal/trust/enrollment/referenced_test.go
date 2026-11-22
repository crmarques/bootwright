package enrollment

import (
	"context"
	"errors"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/trust"
)

type pinObserver struct {
	*observer
	restricted trust.HostKey
}

func (o *pinObserver) ObserveKey(ctx context.Context, _ string, address string, port int, key trust.HostKey) (trust.HostKey, error) {
	o.restricted = key
	return o.Observe(ctx, address, port)
}

func TestReferencedEnrollmentWritesOneCanonicalHostPin(t *testing.T) {
	ctx := context.Background()
	key := trust.HostKey{Type: "ssh-ed25519", PublicKey: publicKey}
	conflict := errors.New("new version appeared")
	previous := key.Line("192.0.2.14", 2222)
	for _, test := range []struct {
		name         string
		input        string
		expected     string
		old          string
		replace      bool
		dry          bool
		yes          bool
		decline      bool
		conflict     bool
		pinHeld      bool
		observeError bool
		keyFileError bool
		want         string
	}{
		{name: "console confirmation", want: ActionAdd},
		{name: "fingerprint", expected: key.Fingerprint(), yes: true, want: ActionAdd},
		{name: "public key", input: key.Type + " " + key.PublicKey + " comment", yes: true, want: ActionAdd},
		{name: "no expectation", yes: true},
		{name: "dry run without expectation", dry: true, want: ActionAdd},
		{name: "wrong fingerprint", expected: (trust.HostKey{Type: key.Type, PublicKey: otherKey}).Fingerprint(), yes: true},
		{name: "private key input", input: "-----BEGIN OPENSSH PRIVATE KEY-----", yes: true},
		{name: "decline", decline: true},
		{name: "moved endpoint", old: previous},
		{name: "key changed", old: (trust.HostKey{Type: key.Type, PublicKey: otherKey}).Line("192.0.2.13", 2222)},
		{name: "held pin", old: previous, replace: true, pinHeld: true},
		{name: "observation failed", observeError: true},
		{name: "file read failed", input: "unreadable", keyFileError: true},
		{name: "file and fingerprint differ", input: key.Type + " " + key.PublicKey, expected: (trust.HostKey{Type: key.Type, PublicKey: otherKey}).Fingerprint(), yes: true},
		{name: "public key header only", input: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5", yes: true},
		{name: "explicit endpoint move", old: previous, replace: true, expected: key.Fingerprint(), yes: true, want: ActionReplace},
		{name: "unchanged", old: key.Line("192.0.2.13", 2222), yes: true, want: ActionReuse},
		{name: "publication conflict", conflict: true, yes: true, expected: key.Fingerprint()},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			if test.observeError {
				h.observer.fail["192.0.2.13"] = errors.New("unreachable")
			}
			o := &pinObserver{observer: h.observer}
			h.service.options.Observer = o
			objects := catalog().Objects()
			target, _ := catalog().Find(api.Machine, "declared")
			spec := target.Spec().WithPath(api.IntegerValue("2222"), "access", "ssh", "port").
				WithPath(api.StringValue("pinned"), "access", "lifecycle", "knownHostsRef")
			objects = append(objects, target.WithSpec(spec))
			var currentVersion, published string
			if test.old != "" {
				currentVersion = "previous"
			}
			h.service.options.Pins = func(_ context.Context, _ string, lookup func(api.Catalog) (string, error)) (PinSnapshot, error) {
				graph := api.NewCatalog(objects[len(objects)-1:])
				ref, err := lookup(graph)
				if err != nil || ref != "pinned" {
					t.Fatalf("selected %q, %v", ref, err)
				}
				return PinSnapshot{Value: []byte(test.old), Version: currentVersion, CheckWrite: func(context.Context) error {
					if test.pinHeld {
						return errors.New("effect holds the pin")
					}
					return nil
				},
					Publish: func(_ context.Context, entry string) (string, error) {
						if test.conflict {
							return "", conflict
						}
						published = entry
						return "new", nil
					}}, nil
			}
			req := EnrollRequest{ReferencedSecret: true, Machines: []string{"declared"}, SkipConfirmation: test.yes, DryRun: test.dry}
			if test.input != "" {
				req.HostPublicKeyFile = "operator.pub"
				h.service.options.PublicKey = func(context.Context, string) ([]byte, error) {
					if test.keyFileError {
						return nil, errors.New("public key is unavailable")
					}
					return []byte(test.input), nil
				}
			}
			if test.expected != "" {
				req.Expect = map[string]string{"declared": test.expected}
			}
			if test.replace {
				req.Replace = []string{"declared"}
			}
			if test.decline {
				h.prompt.decline = errors.New("declined")
			}
			report, err := h.service.Enroll(ctx, req)
			if test.want == "" {
				if test.pinHeld && h.prompt.asked != 0 {
					t.Fatal("an effect hold reached confirmation")
				}
				if err == nil || published != "" || len(h.trust.written) != 0 {
					t.Fatalf("unexpected successful/write result %+v, %v", report, err)
				}
				return
			}
			if err != nil || action(report, "declared").Action != test.want || report.Hosts[0].Secret != "pinned" {
				t.Fatalf("enrollment = %+v, %v", report, err)
			}
			if len(h.observer.addresses()) != 1 || test.input != "" && o.restricted != key {
				t.Fatalf("observations %v, restricted key %v", h.observer.addresses(), o.restricted)
			}
			if test.dry || test.want == ActionReuse {
				if published != "" {
					t.Fatal("preview or reuse wrote a version")
				}
				return
			}
			if published != key.Line("192.0.2.13", 2222) || report.Hosts[0].Version != "new" || len(h.trust.written) != 0 {
				t.Fatalf("stored %q, report %+v", published, report)
			}
			if !test.yes && strings.Join(h.events.log, ",") != "present,confirm" {
				t.Fatalf("confirmation events %v", h.events.log)
			}
		})
	}
}

func TestReferencedEnrollmentRefusesAnOpaqueSecretUsedByAnotherConsumer(t *testing.T) {
	base := catalog()
	machine, _ := base.Find(api.Machine, "declared")
	consumer := api.NewObject(api.CephSMBResource, "share", m(), m("secrets", list(m("path", "/config", "secretRef", "pinned"))))
	_, _, err := referencedTarget(api.NewCatalog([]api.Object{machine, consumer}), "declared")
	if reported := diagnostics.Of(err); len(reported) != 1 || !strings.Contains(reported[0].Message, "secrets[0].secretRef") {
		t.Fatalf("refusal = %v", err)
	}
}

func TestReferencedEnrollmentRejectsMissingOrAmbiguousSelections(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	request := EnrollRequest{ReferencedSecret: true, Machines: []string{"declared"}}
	if _, err := h.service.Enroll(ctx, request); err == nil {
		t.Fatal("missing custody port was accepted")
	}
	h.service.options.Pins = func(_ context.Context, _ string, resolve func(api.Catalog) (string, error)) (PinSnapshot, error) {
		_, err := resolve(catalog())
		return PinSnapshot{}, err
	}
	request.Machines = nil
	if _, err := h.service.Enroll(ctx, request); err == nil {
		t.Fatal("missing target was accepted")
	}
	request.Machines = []string{"declared"}
	request.Expect = map[string]string{"other": "SHA256:example"}
	if _, err := h.service.Enroll(ctx, request); err == nil {
		t.Fatal("an expectation for another Machine was accepted")
	}
	request.Machines, request.Expect = []string{"installed"}, nil
	if _, err := h.service.Enroll(ctx, request); err == nil {
		t.Fatal("an installed host was selected for referenced enrollment")
	}
	request.Machines = []string{"node-a"}
	if _, err := h.service.Enroll(ctx, request); err == nil {
		t.Fatal("a Machine missing its reference was accepted")
	}
	if got := h.observer.addresses(); len(got) != 0 {
		t.Fatalf("invalid selection probed %v", got)
	}
}
