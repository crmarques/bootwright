package inspection

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
)

// --secrets alone reveals only the dashboard password; --object-user <name>
// reveals that one user's keys and nothing else (D183, Q116).
func TestObjectUserCredentialsRequireTheirOwnSelector(t *testing.T) {
	f := newFixture(t)
	objects := append(f.catalog.Objects(), api.NewObject(api.CephObjectUser, "documents", api.Value{}, api.MapValue(field("clusterRef", text("storage")), field("objectGatewayRef", text("objects")))), api.NewObject(api.CephObjectGateway, "objects", api.Value{}, api.MapValue(field("clusterRef", text("storage")))))
	f.catalog = api.NewCatalog(objects)
	f.objectUsers = map[string]Custodied{"documents": {Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte("ACCESS"), secrets.PasswordPart: []byte("PRIVATE")})}}
	result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}})
	if err != nil || len(f.objectReads) != 0 || len(result.Storage[0].Ceph.ObjectUsers) != 0 {
		t.Fatal("ordinary info read private object material", err)
	}
	result, err = f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, Secrets: true})
	if err != nil || len(f.objectReads) != 0 || len(result.Storage[0].Ceph.ObjectUsers) != 0 || !result.Storage[0].Ceph.Dashboard.Revealed {
		t.Fatal("--secrets revealed object material or no dashboard password", err)
	}
	ClearSecrets(result)
	result, err = f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, ObjectUser: "documents"})
	if err != nil || len(f.objectReads) != 1 || len(result.Storage[0].Ceph.ObjectUsers) != 1 || result.Storage[0].Ceph.Dashboard != nil && result.Storage[0].Ceph.Dashboard.Revealed {
		t.Fatal("--object-user did not reveal that user's keys alone", err)
	}
	ClearSecrets(result)
}

func TestObjectUserHandoffRefusesUnprovedCustodyAndClearsOtherSecrets(t *testing.T) {
	f := newFixture(t)
	f.catalog = api.NewCatalog(append(f.catalog.Objects(), api.NewObject(api.CephObjectUser, "documents", api.Value{}, api.MapValue(field("clusterRef", text("storage")), field("objectGatewayRef", text("objects")))), api.NewObject(api.CephObjectGateway, "objects", api.Value{}, api.MapValue(field("clusterRef", text("storage"))))))
	f.objectUsers = map[string]Custodied{"documents": {Unproved: true, Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte("ACCESS"), secrets.PasswordPart: []byte("PRIVATE")})}}
	if result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, ObjectUser: "documents"}); err == nil || result != nil {
		t.Fatal("unproved credentials were revealed")
	}
	for _, material := range []secrets.Material{f.objectUsers["documents"].Material} {
		for _, part := range material.Parts() {
			value, _ := material.Part(part)
			for _, b := range value {
				if b != 0 {
					t.Fatal("refusal retained private material")
				}
			}
		}
	}
}

func handoffUser(name, cluster, gateway string) api.Object {
	return api.NewObject(api.CephObjectUser, name, api.Value{}, api.MapValue(field("clusterRef", text(cluster)), field("objectGatewayRef", text(gateway))))
}

func handoffMaterial() secrets.Material {
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte("ACCESS"), secrets.PasswordPart: []byte("PRIVATE")})
}

func assertHandoffCleared(t *testing.T, materials ...secrets.Material) {
	t.Helper()
	for _, material := range materials {
		for _, part := range material.Parts() {
			value, _ := material.Part(part)
			for _, b := range value {
				if b != 0 {
					t.Fatal("private material survived refusal")
				}
			}
		}
	}
}

func TestObjectUserHandoffDoesNotReadForeignClusterCustody(t *testing.T) {
	f := newFixture(t)
	f.catalog = api.NewCatalog(append(f.catalog.Objects(), handoffUser("foreign", "archive", "absent-gateway")))
	result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, ObjectUser: "foreign"})
	if reported := diagnostics.Of(err); len(reported) == 0 || reported[0].Code != "access.target" || result != nil || len(f.objectReads) != 0 {
		t.Fatal("foreign user reached custody", err, f.objectReads)
	}
}

func TestObjectUserHandoffRefusesChangedScopeBeforeCustody(t *testing.T) {
	for _, scenario := range []string{"gateway-other-cluster", "missing-gateway", "unproved-original-scope"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			gatewayCluster := "storage"
			if scenario == "gateway-other-cluster" {
				gatewayCluster = "archive"
			}
			objects := append(f.catalog.Objects(), handoffUser("documents", "storage", "objects"))
			if scenario != "missing-gateway" {
				objects = append(objects, api.NewObject(api.CephObjectGateway, "objects", api.Value{}, api.MapValue(field("clusterRef", text(gatewayCluster)))))
			}
			f.catalog = api.NewCatalog(objects)
			f.objectUnproved = scenario == "unproved-original-scope"
			result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, ObjectUser: "documents"})
			if err == nil || result != nil || len(f.objectReads) != 0 {
				t.Fatal("changed scope read custody", err, f.objectReads)
			}
		})
	}
}

// Only the selected user's custody is read: a refusal of it reads no other
// user's, and a name the cluster does not declare reads nothing.
func TestObjectUserHandoffReadsOnlyTheSelectedUser(t *testing.T) {
	for _, scenario := range []string{"missing", "unproved", "malformed", "undeclared"} {
		t.Run(scenario, func(t *testing.T) {
			f := newFixture(t)
			f.catalog = api.NewCatalog(append(f.catalog.Objects(), handoffUser("z-last", "storage", "objects"), handoffUser("a-first", "storage", "objects"), api.NewObject(api.CephObjectGateway, "objects", api.Value{}, api.MapValue(field("clusterRef", text("storage"))))))
			first, last := handoffMaterial(), handoffMaterial()
			f.objectUsers = map[string]Custodied{"a-first": {Material: first}}
			if scenario == "unproved" {
				f.objectUsers["z-last"] = Custodied{Material: last, Unproved: true}
			}
			if scenario == "malformed" {
				last = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("PRIVATE")})
				f.objectUsers["z-last"] = Custodied{Material: last}
			}
			selected, reads := "z-last", []string{"lab/z-last"}
			if scenario == "undeclared" {
				selected, reads = "absent", nil
			}
			result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, ObjectUser: selected})
			if err == nil || result != nil || !slices.Equal(f.objectReads, reads) {
				t.Fatal("the refusal read other custody", err, f.objectReads)
			}
			if scenario != "missing" && scenario != "undeclared" {
				assertHandoffCleared(t, last)
			}
			last.Clear()
			first.Clear()
		})
	}
}

// A proof or custody read that fails is returned, custody the store flags as
// not one S3 credential is refused as corrupt with its material cleared, and
// a credential outside its bounds is refused.
func TestObjectUserHandoffReturnsFailedReadsAndRefusesMalformedCredentials(t *testing.T) {
	setup := func() *fixture {
		f := newFixture(t)
		f.catalog = api.NewCatalog(append(f.catalog.Objects(), handoffUser("documents", "storage", "objects"), api.NewObject(api.CephObjectGateway, "objects", api.Value{}, api.MapValue(field("clusterRef", text("storage"))))))
		return f
	}
	ask := func(f *fixture) (*InfoResult, error) {
		return f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, ObjectUser: "documents"})
	}
	failure := errors.New("the store is unreadable")
	f := setup()
	f.objectProofErr = failure
	if result, err := ask(f); result != nil || !errors.Is(err, failure) || len(f.objectReads) != 0 {
		t.Fatalf("a failing proof = %+v (%v), read %v", result, err, f.objectReads)
	}
	f = setup()
	f.readError = failure
	f.objectUsers = map[string]Custodied{"documents": {Material: handoffMaterial()}}
	if result, err := ask(f); result != nil || !errors.Is(err, failure) {
		t.Fatalf("a failing custody read = %+v (%v)", result, err)
	}
	assertHandoffCleared(t, f.objectUsers["documents"].Material)
	f = setup()
	f.readError = fmt.Errorf("custody: %w", ErrObjectUserMaterial)
	f.objectUsers = map[string]Custodied{"documents": {Material: handoffMaterial()}}
	result, err := ask(f)
	if reported := diagnostics.Of(err); result != nil || len(reported) != 1 || reported[0].Code != "secret.store.corrupt" || !strings.Contains(reported[0].Message, "documents") {
		t.Fatalf("flagged material = %+v (%v)", result, err)
	}
	assertHandoffCleared(t, f.objectUsers["documents"].Material)
}

func TestAnS3CredentialIsOneBoundedPrintableLoginOfItsUser(t *testing.T) {
	login := func(user, password string) secrets.Material {
		return secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte(user), secrets.PasswordPart: []byte(password)})
	}
	for name, test := range map[string]struct {
		material secrets.Material
		want     bool
	}{
		"a login":                {login("ACCESS", "PRIVATE"), true},
		"the longest value":      {login(strings.Repeat("a", 128), strings.Repeat("b", 128)), true},
		"an empty user":          {login("", "PRIVATE"), false},
		"an empty password":      {login("ACCESS", ""), false},
		"a value past its bound": {login("ACCESS", strings.Repeat("b", 129)), false},
		"a space":                {login("ACC ESS", "PRIVATE"), false},
		"a control":              {login("ACCESS", "PRI\nVATE"), false},
		"a password alone":       {secrets.NewMaterial(map[secrets.Part][]byte{secrets.PasswordPart: []byte("PRIVATE")}), false},
		"a third part":           {secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte("A"), secrets.PasswordPart: []byte("P"), secrets.ValuePart: []byte("V")}), false},
	} {
		if got := validObjectCredentials(test.material); got != test.want {
			t.Errorf("%s valid = %v, want %v", name, got, test.want)
		}
	}
}

// ClearSecrets clears every secret an inspection revealed, a cluster's
// console password, a dashboard password and the object users' keys
// included, and passes a storage cluster with no managed Ceph.
func TestClearSecretsClearsEveryRevealedSecret(t *testing.T) {
	console, dashboard, keys := handoffMaterial(), handoffMaterial(), handoffMaterial()
	result := &InfoResult{
		Clusters: []ContainerEntry{{Name: "edge"}, {Name: "okd", Details: &ContainerDetails{Console: &ConsoleAccess{Password: console}}}},
		Storage:  []StorageEntry{{Name: "bare"}, {Name: "storage", Ceph: &ManagedCeph{Dashboard: &Dashboard{Password: dashboard}, ObjectUsers: []ObjectUserAccess{{Name: "documents", Credentials: keys}}}}},
	}
	ClearSecrets(result)
	assertHandoffCleared(t, console, dashboard, keys)
}
