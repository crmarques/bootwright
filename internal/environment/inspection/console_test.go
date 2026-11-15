package inspection

import (
	"context"
	"testing"

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
