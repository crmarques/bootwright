package inspection

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"

	"github.com/crmarques/bootwright/internal/secrets"
)

type consoleFake struct {
	calls            int
	unproved, absent bool
}

func (f *consoleFake) ConsolePassword(context.Context, string, string) (Custodied, bool, error) {
	f.calls++
	return Custodied{Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("synthetic-console-password-12345")}), Unproved: f.unproved}, !f.absent, nil
}

func TestConsoleSecretsAreExplicitAndConfinedToOneCluster(t *testing.T) {
	for _, cluster := range []string{"edge", "okd-lab"} {
		fixture := newFixture(t)
		custody := &consoleFake{}
		service := fixture.service("lab").WithConsoleCustody(custody)
		ordinary, err := service.ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{cluster}})
		if err != nil || custody.calls != 0 {
			t.Fatalf("ordinary inspection: %+v %v", ordinary, err)
		}
		revealed, err := service.ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{cluster}, Secrets: true})
		if err != nil || custody.calls != 1 || len(revealed.Clusters) != 1 || len(revealed.Storage) != 0 {
			t.Fatalf("reveal: %+v %v", revealed, err)
		}
		console := revealed.Clusters[0].Details.Console
		if console == nil || console.User != "kubeadmin" || !console.Revealed {
			t.Fatalf("console = %+v", console)
		}
		ClearSecrets(revealed)
		if value, _ := console.Password.Part(secrets.ValuePart); string(value) == "synthetic-console-password-12345" {
			clear(value)
			t.Fatal("console material was retained after clear")
		}
	}
}

func TestConsoleSecretsRefuseAbsentOrUnprovedCustodyWithoutDisclosure(t *testing.T) {
	for _, custody := range []*consoleFake{{absent: true}, {unproved: true}} {
		fixture := newFixture(t)
		result, err := fixture.service("lab").WithConsoleCustody(custody).ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"edge"}, Secrets: true})
		if result != nil || err == nil {
			t.Fatalf("unproved reveal = %+v %v", result, err)
		}
	}
}

type scriptedConsole struct {
	held  Custodied
	found bool
	err   error
}

func (c scriptedConsole) ConsolePassword(context.Context, string, string) (Custodied, bool, error) {
	return c.held, c.found, c.err
}

func consolePassword(parts map[secrets.Part][]byte) Custodied {
	return Custodied{Material: secrets.NewMaterial(parts)}
}

// A console reveal with no custody wired refuses, a custody that fails
// returns its error, and a password outside the generated grammar, a
// password of another length or material of another shape is refused as
// corrupt custody, each clearing the material it read.
func TestConsoleRevealRefusesWhatCustodyCannotProve(t *testing.T) {
	ask := func(service Service) (*InfoResult, error) {
		return service.ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"edge"}, Secrets: true})
	}
	if result, err := ask(newFixture(t).service("lab")); result != nil || stopCodeOf(t, err) != "access.unavailable" || !strings.Contains(diagnostics.Of(err)[0].Remediation, "auth/kubeadmin-password") {
		t.Fatalf("no custody = %+v (%v)", result, err)
	}
	failure := errors.New("the custody store is unreadable")
	held := consolePassword(map[secrets.Part][]byte{secrets.ValuePart: []byte(strings.Repeat("a", 23))})
	if result, err := ask(newFixture(t).service("lab").WithConsoleCustody(scriptedConsole{held: held, found: true, err: failure})); result != nil || !errors.Is(err, failure) || !cleared(held.Material) {
		t.Fatalf("a failing custody = %+v (%v)", result, err)
	}
	for name, parts := range map[string]map[secrets.Part][]byte{
		"too short":    {secrets.ValuePart: []byte(strings.Repeat("a", 22))},
		"too long":     {secrets.ValuePart: []byte(strings.Repeat("a", 129))},
		"a space":      {secrets.ValuePart: []byte(strings.Repeat("a", 22) + " ")},
		"a control":    {secrets.ValuePart: []byte(strings.Repeat("a", 22) + "\x7f")},
		"another part": {secrets.UsernamePart: []byte(strings.Repeat("a", 23))},
		"two parts":    {secrets.ValuePart: []byte(strings.Repeat("a", 23)), secrets.UsernamePart: []byte("kubeadmin")},
	} {
		held := consolePassword(parts)
		result, err := ask(newFixture(t).service("lab").WithConsoleCustody(scriptedConsole{held: held, found: true}))
		if result != nil || stopCodeOf(t, err) != "secret.store.corrupt" || !cleared(held.Material) {
			t.Errorf("%s = %+v (%v)", name, result, err)
		}
	}
	for _, length := range []int{23, 128} {
		held := consolePassword(map[secrets.Part][]byte{secrets.ValuePart: []byte(strings.Repeat("a", length))})
		result, err := ask(newFixture(t).service("lab").WithConsoleCustody(scriptedConsole{held: held, found: true}))
		if err != nil || result == nil || !result.Clusters[0].Details.Console.Revealed {
			t.Fatalf("a password of %d characters = %+v (%v)", length, result, err)
		}
		ClearSecrets(result)
	}
}

func stopCodeOf(t *testing.T, err error) string {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 {
		t.Fatalf("diagnostics = %+v (%v)", reported, err)
	}
	return reported[0].Code
}
