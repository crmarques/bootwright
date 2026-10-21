package ceph

import (
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

type NFSExportClient struct {
	AccessType string   `json:"access_type"`
	Addresses  []string `json:"addresses"`
	Squash     string   `json:"squash"`
}

type NFSExportFSAL struct {
	CmountPath string `json:"cmount_path,omitempty"`
	FSName     string `json:"fs_name,omitempty"`
	Name       string `json:"name"`
	UserID     string `json:"user_id,omitempty"`
}

type NFSExportNative struct {
	AccessType    string            `json:"access_type"`
	Clients       []NFSExportClient `json:"clients"`
	FSAL          NFSExportFSAL     `json:"fsal"`
	Path          string            `json:"path,omitempty"`
	Protocols     []int             `json:"protocols"`
	Pseudo        string            `json:"pseudo"`
	SecType       []string          `json:"sectype,omitempty"`
	SecurityLabel bool              `json:"security_label"`
	Squash        string            `json:"squash"`
	Transports    []string          `json:"transports"`
}

type NFSExportObjectUser struct {
	Gateway   string `json:"gateway"`
	Name      string `json:"name"`
	Realm     string `json:"realm"`
	Zone      string `json:"zone"`
	ZoneGroup string `json:"zoneGroup"`
}

type NFSExport struct {
	Filesystem    string               `json:"filesystem"`
	Group         string               `json:"group"`
	Name          string               `json:"name"`
	Native        NFSExportNative      `json:"native"`
	ObjectUser    *NFSExportObjectUser `json:"objectUser,omitempty"`
	Service       string               `json:"service"`
	Subvolume     string               `json:"subvolume"`
	SubvolumePath string               `json:"subvolumePath,omitempty"`
}

func NFSExportBlockID(name string) string { return "ceph-nfs-export-" + name }

func nfsExportNativeOf(native api.Value, filesystem string) NFSExportNative {
	out := NFSExportNative{AccessType: native.Get("access_type").Text(), Clients: []NFSExportClient{}, Path: native.Get("path").Text(), Protocols: []int{},
		Pseudo: native.Get("pseudo").Text(), SecurityLabel: native.Get("security_label").Bool(), Squash: native.Get("squash").Text(), Transports: native.Get("transports").Strings(),
		FSAL: NFSExportFSAL{CmountPath: native.Get("fsal", "cmount_path").Text(), FSName: filesystem, Name: native.Get("fsal", "name").Text()}}
	for _, protocol := range native.Get("protocols").Items() {
		if value, ok := protocol.Int64(); ok {
			out.Protocols = append(out.Protocols, int(value))
		}
	}
	if out.Transports == nil {
		out.Transports = []string{}
	}
	if native.Has("sectype") {
		out.SecType = native.Get("sectype").Strings()
	}
	for _, client := range native.Get("clients").Items() {
		addresses := client.Get("addresses").Strings()
		if addresses == nil {
			addresses = []string{}
		}
		out.Clients = append(out.Clients, NFSExportClient{AccessType: client.Get("access_type").Text(), Addresses: addresses, Squash: client.Get("squash").Text()})
	}
	return out
}

func nfsExportRequest(c api.Catalog, export api.Object, contextName string) (Request, error) {
	name, spec := export.Name(), export.Spec()
	if !reconciliation.ValidSegment(NFSExportBlockID(name)) {
		return Request{}, failure("api.invariant", "StorageNFSExport "+name+" has a name too long to identify its block, whose identity holds at most 63 characters", "rename the StorageNFSExport to at most 47 characters")
	}
	var r Request
	frozen := NFSExport{Name: name, Service: spec.Get("serviceID").Text()}
	if ref := spec.Get("objectUserRef").Text(); ref != "" {
		user, _ := c.Find(api.StorageObjectUser, ref)
		request, err := objectUserRequest(c, user, contextName)
		if err != nil {
			return Request{}, err
		}
		u := request.ObjectUser
		r, frozen.ObjectUser = request, &NFSExportObjectUser{Gateway: u.Gateway, Name: u.Name, Realm: u.Realm, Zone: u.Zone, ZoneGroup: u.ZoneGroup}
	} else if ref := spec.Get("subvolumeRef").Text(); ref != "" {
		subvolume, _ := c.Find(api.StorageSubvolume, ref)
		request, err := subvolumeRequest(c, subvolume, contextName)
		if err != nil {
			return Request{}, err
		}
		r, frozen.Filesystem, frozen.Group, frozen.Subvolume = request, request.Subvolume.Filesystem, request.Subvolume.Group, request.Subvolume.Name
	} else {
		fs, _ := c.Find(api.StorageFilesystem, spec.Get("filesystemRef").Text())
		request, err := filesystemRequest(c, fs, contextName)
		if err != nil {
			return Request{}, err
		}
		r, frozen.Filesystem = request, request.Filesystem.Name
	}
	cluster, _ := c.Find(api.StorageCluster, r.Cluster)
	units, err := nfsUnits(cluster)
	if err != nil {
		return Request{}, err
	}
	declared := false
	for _, unit := range units {
		declared = declared || unit.ID == frozen.Service
	}
	if !declared {
		return Request{}, failure("api.invariant", "StorageNFSExport "+name+"'s spec.serviceID resolves to no declared nfs service of StorageCluster "+r.Cluster,
			"add a spec.ceph.services entry with service_type nfs and this service_id to StorageCluster "+r.Cluster+", or set spec.serviceID to the service_id of a declared nfs service")
	}
	frozen.Native = nfsExportNativeOf(spec.Get("native"), frozen.Filesystem)
	if frozen.ObjectUser != nil {
		frozen.Native.FSAL = NFSExportFSAL{Name: "RGW", UserID: frozen.ObjectUser.Name}
	}
	r.Action, r.Filesystem, r.Subvolume, r.Pool, r.ObjectUser, r.NFSExport = "nfs-export", nil, nil, nil, nil, &frozen
	return r, nil
}

func nfsExportDeadline(operation string) time.Duration {
	budget := map[string]unitBudget{"apply": {calls: 2, waits: 1}, "observe": {calls: 1}, "destroy": {calls: 2}}[operation]
	return time.Duration(budget.calls*nativeCall+budget.waits*readinessWait+60) * time.Second
}
