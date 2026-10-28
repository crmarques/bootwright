package ceph

import (
	"context"
	"encoding/base64"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const (
	clientSubvolumePath = "/volumes/csi/docs/0f8fad5b-d9cb-469f-a165-70867728950e"
	clientNoGroupPath   = "/volumes/_nogroup/docs/0f8fad5b-d9cb-469f-a165-70867728950e"
)

func filesystemClientCatalog() api.Catalog {
	return filesystemCatalog(withMDS, filesystemOf(), childOf(api.CephClient, "reader", "filesystemRef", "files", "access", "readOnly"))
}

func subvolumeClientCatalog(change func(api.Value) api.Value) api.Catalog {
	return filesystemCatalog(change, filesystemOf("subvolumeGroups", api.ListValue(requestMap("name", "csi"))), subvolumeOf("group", "csi"),
		childOf(api.CephClient, "writer", "subvolumeRef", "docs", "access", "readWrite"))
}

func noGroupClientCatalog() api.Catalog {
	return filesystemCatalog(withMDS, filesystemOf(), subvolumeOf(), childOf(api.CephClient, "writer", "subvolumeRef", "docs", "access", "readWrite"))
}

func TestClientCapsPerArm(t *testing.T) {
	filesystem := Client{Access: "readWrite", DataPools: []string{}, Filesystem: "files", Name: "reader"}
	subvolume := Client{Access: "readWrite", DataPools: []string{}, Filesystem: "files", Group: "csi", Name: "writer", Subvolume: "docs", SubvolumePath: clientSubvolumePath}
	change := func(c Client, edit func(*Client)) Client {
		edit(&c)
		return c
	}
	readOnly := func(c *Client) { c.Access = "readOnly" }
	for _, tt := range []struct {
		name   string
		client Client
		want   map[string]string
	}{
		{"rbd read-write", Client{Access: "readWrite", DataPools: []string{}, Name: "consumer", Pool: "volumes"},
			map[string]string{"mon": "profile rbd", "mgr": "profile rbd pool=volumes", "osd": "profile rbd pool=volumes"}},
		{"filesystem read-write", filesystem,
			map[string]string{"mds": "allow rw fsname=files", "mon": "allow r fsname=files", "osd": "allow rw tag cephfs data=files"}},
		{"filesystem read-only", change(filesystem, readOnly),
			map[string]string{"mds": "allow r fsname=files", "mon": "allow r fsname=files", "osd": "allow r tag cephfs data=files"}},
		{"subvolume read-write", subvolume,
			map[string]string{"mds": "allow rw fsname=files path=" + clientSubvolumePath, "mon": "allow r fsname=files", "osd": "allow rw tag cephfs data=files"}},
		{"subvolume read-only", change(subvolume, readOnly),
			map[string]string{"mds": "allow r fsname=files path=" + clientSubvolumePath, "mon": "allow r fsname=files", "osd": "allow r tag cephfs data=files"}},
		{"subvolume without group", change(subvolume, func(c *Client) { c.Group, c.SubvolumePath = "", clientNoGroupPath }),
			map[string]string{"mds": "allow rw fsname=files path=" + clientNoGroupPath, "mon": "allow r fsname=files", "osd": "allow rw tag cephfs data=files"}},
		{"bad access", change(filesystem, func(c *Client) { c.Access = "rw" }), nil},
		{"pool with filesystem", change(filesystem, func(c *Client) { c.Pool = "volumes" }), nil},
		{"group without subvolume", change(filesystem, func(c *Client) { c.Group = "csi" }), nil},
		{"subvolume without filesystem", change(subvolume, func(c *Client) { c.Filesystem = "" }), nil},
		{"filesystem with data pools", change(filesystem, func(c *Client) { c.DataPools = []string{"fs-data"} }), nil},
		{"subvolume without path", change(subvolume, func(c *Client) { c.SubvolumePath = "" }), nil},
		{"filesystem with path", change(filesystem, func(c *Client) { c.SubvolumePath = clientSubvolumePath }), nil},
		{"pool with path", Client{Access: "readWrite", DataPools: []string{}, Name: "consumer", Pool: "volumes", SubvolumePath: clientSubvolumePath}, nil},
		{"no arm", Client{Access: "readWrite", DataPools: []string{}, Name: "consumer"}, nil},
	} {
		got, ok := clientCaps(tt.client)
		if ok != (tt.want != nil) || !maps.Equal(got, tt.want) {
			t.Errorf("%s: caps %v %v, want %v", tt.name, got, ok, tt.want)
		}
	}
}

func refuseProduced(t *testing.T, name string, request Request, entity string, caps map[string]string) {
	t.Helper()
	bad := clientProduced(t, entity, caps)
	err := validateProduced(bad, request, true)
	lifecycle.ClearProduced(bad)
	if err == nil {
		t.Errorf("%s accepted", name)
	} else if strings.Contains(err.Error(), "private-key") || strings.Contains(err.Error(), base64.StdEncoding.EncodeToString([]byte("private-key"))) {
		t.Errorf("%s: the key reached the error", name)
	}
}

func TestProducedCephFSClientRejectsWiderCaps(t *testing.T) {
	for _, client := range []Client{
		{Access: "readWrite", DataPools: []string{}, Filesystem: "files", Name: "writer"},
		{Access: "readWrite", DataPools: []string{}, Filesystem: "files", Group: "csi", Name: "writer", Subvolume: "docs", SubvolumePath: clientSubvolumePath},
	} {
		r := coreRequest()
		r.Action, r.Client = "client", &client
		derived, _ := clientCaps(client)
		good := clientProduced(t, "client.writer", derived)
		if err := validateProduced(good, r, true); err != nil {
			t.Fatalf("the derived caps of %+v were refused: %v", client, err)
		}
		lifecycle.ClearProduced(good)
		with := func(name, value string) map[string]string {
			changed := maps.Clone(derived)
			changed[name] = value
			return changed
		}
		refuseProduced(t, "mds without path", r, "client.writer", with("mds", "allow rw"))
		refuseProduced(t, "another subvolume's path", r, "client.writer", with("mds", "allow rw fsname=files path=/volumes/csi/other/"+subvolumeID))
		refuseProduced(t, "a second mds grant", r, "client.writer", with("mds", derived["mds"]+", allow rw"))
		refuseProduced(t, "every data pool", r, "client.writer", with("osd", "allow rw tag cephfs data=*"))
		refuseProduced(t, "a mgr cap", r, "client.writer", with("mgr", "allow r"))
		refuseProduced(t, "osd allow *", r, "client.writer", with("osd", "allow *"))
		refuseProduced(t, "client.admin", r, "client.admin", derived)
		if client.Subvolume != "" {
			refuseProduced(t, "the filesystem-wide mds grant", r, "client.writer", with("mds", "allow rw fsname=files"))
			unbound := client
			unbound.SubvolumePath = ""
			request := r
			request.Client = &unbound
			refuseProduced(t, "an unbound subvolume request", request, "client.writer", derived)
		}
	}
}

func TestClientPlanStatesScope(t *testing.T) {
	for _, tt := range []struct {
		catalog  api.Catalog
		name     string
		requires reconciliation.ObjectRef
		frozen   Client
		scope    string
	}{
		{filesystemClientCatalog(), "reader", reconciliation.ObjectRef{Kind: string(api.CephFilesystem), Object: "files"},
			Client{Access: "readOnly", Filesystem: "files", Name: "reader"}, "client.reader read-only on filesystem files"},
		{subvolumeClientCatalog(withMDS), "writer", reconciliation.ObjectRef{Kind: string(api.CephSubvolume), Object: "docs"},
			Client{Access: "readWrite", Filesystem: "files", Group: "csi", Name: "writer", Subvolume: "docs"}, "client.writer read-write on subvolume docs in group csi of filesystem files"},
		{noGroupClientCatalog(), "writer", reconciliation.ObjectRef{Kind: string(api.CephSubvolume), Object: "docs"},
			Client{Access: "readWrite", Filesystem: "files", Name: "writer", Subvolume: "docs"}, "client.writer read-write on subvolume docs of filesystem files"},
	} {
		definition := clientPlan(t, tt.catalog)
		r, err := DecodeRequest(definition.Request)
		if err != nil || r.Client == nil || r.Pool != nil || r.Filesystem != nil || r.Subvolume != nil || r.Action != "client" {
			t.Fatalf("the %s request decoded as %+v (%v)", tt.name, r, err)
		}
		if !slices.Equal(definition.Requires, []reconciliation.ObjectRef{tt.requires}) {
			t.Fatalf("the %s client requires %v", tt.name, definition.Requires)
		}
		if want := []string{ClusterBlockID("example"), ImageBlockID("example")}; !slices.Equal(definition.Dependencies, want) {
			t.Fatalf("the %s client depends on %v, want %v", tt.name, definition.Dependencies, want)
		}
		got := *r.Client
		if got.DataPools == nil || len(got.DataPools) != 0 {
			t.Fatalf("the %s client froze data pools %v", tt.name, got.DataPools)
		}
		got.DataPools = nil
		if !reflect.DeepEqual(got, tt.frozen) {
			t.Fatalf("the %s client froze %+v", tt.name, got)
		}
		frozen := []reconciliation.Block{frozenScopeBlock(t, api.CephClient, tt.name, r)}
		if got := ResourceScope(tt.catalog, api.CephClient, tt.name, frozen); got != tt.scope {
			t.Fatalf("the frozen %s scope rendered %q", tt.name, got)
		}
	}
}

func TestClientPropagatesCephFSPlanRefusals(t *testing.T) {
	const message, remedy = "CephCluster example declares no single MDS service mds.files, which spec.mdsServiceRef of CephFilesystem files names", "declare the mds service entry with service_id files in spec.services"
	_, err := planned(t, filesystemCatalog(nil, filesystemOf(), childOf(api.CephClient, "reader", "filesystemRef", "files", "access", "readOnly")), api.CephClient, "reader")
	refusedWith(t, err, "api.invariant", message, remedy)
	_, err = planned(t, subvolumeClientCatalog(nil), api.CephClient, "writer")
	refusedWith(t, err, "api.invariant", message, remedy)
}

func cephfsClientExecution(t *testing.T, r Request) lifecycle.Execution {
	t.Helper()
	e := executionFor(t, r)
	e.Block.ID, e.Block.Kind, e.Block.Object = ClientBlockID(r.Client.Name), string(api.CephClient), r.Client.Name
	return e
}

func plannedClient(t *testing.T, catalog api.Catalog, name string) Request {
	t.Helper()
	r, err := planned(t, catalog, api.CephClient, name)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return r
}

func TestClientPrepareBindsTheProvedSubvolumePath(t *testing.T) {
	r := plannedClient(t, subvolumeClientCatalog(withMDS), "writer")
	e := cephfsClientExecution(t, r)
	e.ApplyProved = append(e.ApplyProved, subvolumeProof(t, "docs", "csi", clientSubvolumePath))
	prepared, err := New(api.CephClient, nil).prepare(context.Background(), e)
	if err != nil || prepared.Client.SubvolumePath != clientSubvolumePath || r.Client.SubvolumePath != "" {
		t.Fatalf("the client bound %+v (%v)", prepared.Client, diagnostics.Of(err))
	}
	if decoded, err := DecodeRequest(e.Block.Request); err != nil || decoded.Client.SubvolumePath != "" {
		t.Fatalf("the frozen request carries %+v (%v)", decoded.Client, err)
	}
	const other, otherRemedy = "the creation proof of CephSubvolume docs names another filesystem or group than CephClient writer froze", "restore the original apply evidence"
	e = cephfsClientExecution(t, r)
	e.ApplyProved = append(e.ApplyProved, subvolumeProof(t, "docs", "other", "/volumes/other/docs/"+subvolumeID))
	_, err = New(api.CephClient, nil).prepare(context.Background(), e)
	refusedWith(t, err, "lifecycle.state", other, otherRemedy)
	e = cephfsClientExecution(t, r)
	e.ApplyProved = append(e.ApplyProved, subvolumeProofOn(t, "other", subvolumeProof(t, "docs", "csi", clientSubvolumePath)))
	_, err = New(api.CephClient, nil).prepare(context.Background(), e)
	refusedWith(t, err, "lifecycle.state", other, otherRemedy)
	_, err = New(api.CephClient, nil).prepare(context.Background(), cephfsClientExecution(t, r))
	refusedWith(t, err, "lifecycle.state", "CephSubvolume docs lacks its applied creation proof", "apply CephSubvolume docs first; a consumer binds the path its creation reported and never constructs one")
	noGroup := cephfsClientExecution(t, plannedClient(t, noGroupClientCatalog(), "writer"))
	noGroup.ApplyProved = append(noGroup.ApplyProved, subvolumeProof(t, "docs", "", clientNoGroupPath))
	if prepared, err := New(api.CephClient, nil).prepare(context.Background(), noGroup); err != nil || prepared.Client.SubvolumePath != clientNoGroupPath {
		t.Fatalf("the client without a group bound %+v (%v)", prepared.Client, diagnostics.Of(err))
	}
	fs := plannedClient(t, filesystemClientCatalog(), "reader")
	if prepared, err := New(api.CephClient, nil).prepare(context.Background(), cephfsClientExecution(t, fs)); err != nil || prepared.Client.SubvolumePath != "" {
		t.Fatalf("the filesystem client prepared %+v (%v)", prepared.Client, diagnostics.Of(err))
	}
	carried := r
	client := *r.Client
	client.SubvolumePath = clientSubvolumePath
	carried.Client = &client
	refusedIdentity(t, api.CephClient, cephfsClientExecution(t, carried), "a planned path")
}
