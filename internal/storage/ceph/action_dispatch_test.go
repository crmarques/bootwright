package ceph

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

var admittedActions = []string{"cluster", "pool", "client", "policy", "rbd", "filesystem", "subvolume", "settings", "service", "gateway", "nfs-export", "object-user", "object-bucket", "nvmeof"}

const dispatchUUID = "0123abcd-0123-4abc-8def-0123456789ab"

var dispatchArms = map[string]func(*Request){
	"settings": func(r *Request) { r.Settings = &Settings{} },
	"service": func(r *Request) {
		r.Service = &ServiceUnit{Type: "nfs", ID: "files", Documents: dispatchDocuments("nfs", "files")}
	},
	"gateway": func(r *Request) {
		r.Gateway = &Gateway{Name: "object", Endpoint: "http://endpoint", Documents: dispatchDocuments("rgw", "object")}
	},
	"policy":     func(r *Request) { r.Policy = &Policy{Name: "object"} },
	"pool":       func(r *Request) { r.Pool = &Pool{Name: "object"} },
	"client":     func(r *Request) { r.Client = &Client{Name: "object"} },
	"rbd":        func(r *Request) { r.RBD = &RBDImage{Name: "object"} },
	"filesystem": func(r *Request) { r.Filesystem = &Filesystem{Name: "object", DataPools: []string{"data"}} },
	"subvolume":  func(r *Request) { r.Subvolume = &Subvolume{Name: "object", Filesystem: "files"} },
	"nvmeof": func(r *Request) {
		r.NVMeOF = &NVMeOFSubsystem{Name: "object", Service: "block", NQN: "nqn.2014-08.org.example:object"}
	},
	"nfs-export": func(r *Request) {
		r.NFSExport = &NFSExport{Name: "object", Service: "files", Native: NFSExportNative{Pseudo: "/pseudo"}}
	},
	"object-user":   func(r *Request) { r.ObjectUser = &ObjectUser{Name: "object"} },
	"object-bucket": func(r *Request) { r.ObjectBucket = &ObjectBucket{Name: "object"} },
}

func dispatchDocuments(serviceType, id string) []json.RawMessage {
	return []json.RawMessage{json.RawMessage(`{"placement":{"hosts":["node-1"]},"service_id":"` + id + `","service_type":"` + serviceType + `"}`)}
}

type dispatchCase struct {
	name    string
	request Request
}

func dispatchCases() []dispatchCase {
	cases := []dispatchCase{}
	for _, action := range append(slices.Clone(admittedActions), "unknown") {
		own := coreRequest()
		own.Action = action
		if arm, found := dispatchArms[action]; found {
			arm(&own)
			bare := coreRequest()
			bare.Action = action
			cases = append(cases, dispatchCase{action + "/bare", bare})
		}
		cases = append(cases, dispatchCase{action + "/own", own})
		foreign := "pool"
		if action == "pool" {
			foreign = "client"
		}
		extra := own
		dispatchArms[foreign](&extra)
		cases = append(cases, dispatchCase{action + "/+" + foreign, extra})
	}
	for i := range cases {
		cases[i].request.Owner = "apply-original"
		cases[i].request.FSID = FSIDFor("apply-original", cases[i].request.Cluster)
	}
	return cases
}

func dispatchEvidences(r Request) []Evidence {
	subvolume := subvolumePath(&Subvolume{Name: "object", Filesystem: "files"}, dispatchUUID)
	resources := []ResourceEvidence{{}, {ID: "7"}, {ID: "0"}, {ID: "abc"}, {ID: dispatchUUID}, {ID: dispatchUUID, Path: subvolume}, {ID: "object"},
		{ID: "7", Path: "/pseudo"}, {ID: dispatchUUID, Path: "http://endpoint"}, {ID: nvmeofSerial(r)}, {Path: "/x"}}
	out := []Evidence{}
	for _, resource := range resources {
		for _, client := range []string{"", "client.object", "client.other"} {
			for _, daemons := range []bool{false, true} {
				for _, reachable := range []bool{false, true} {
					e := Evidence{Version: EvidenceVersion, Action: r.Action, Owner: r.Owner, FSID: r.FSID, Request: strings.Repeat("b", 64), Owned: true, Exists: true, Ready: true,
						Health: "HEALTH_OK", Hosts: []string{"node-1", "node-2", "node-3"}, Monitors: []string{"node-1", "node-2", "node-3"}, MGRActive: "node-1", MGRStandbys: []string{"node-2"},
						Resource: resource, Client: client, Daemons: []UnitDaemon{}, MonAddrs: []MonitorAddress{}}
					for _, host := range e.Hosts {
						e.OSDs = append(e.OSDs, OSDEvidence{Host: host, Path: "/dev/vdb", ID: len(e.OSDs), UUID: "uuid-" + host})
					}
					if daemons {
						for _, d := range unitDocuments(r) {
							e.Daemons = append(e.Daemons, UnitDaemon{Service: documentName(d), Host: "node-1", Name: documentName(d) + ".node-1.abc"})
						}
					}
					if reachable && (r.Action == "gateway" || r.Action == "service") {
						e.Endpoint = EndpointEvidence{Reachable: true}
					}
					out = append(out, e)
				}
			}
		}
	}
	return out
}

func guarded(f func() string) (out string) {
	defer func() {
		if recover() != nil {
			out = "panic"
		}
	}()
	return f()
}

func bits(values []bool) string {
	var b strings.Builder
	for _, v := range values {
		b.WriteByte("01"[map[bool]int{false: 0, true: 1}[v]])
	}
	return b.String()
}

func dispatchIdentity(t *testing.T, r Request) string {
	if _, err := r.Canonical(); err != nil {
		return "unfrozen"
	}
	kinds := []api.Kind{api.CephCluster, api.CephPlacementPolicy, api.CephPool, api.CephClient, api.CephRBDImage, api.CephFilesystem, api.CephSubvolume, api.CephObjectGateway,
		api.CephNFSExport, api.CephObjectUser, api.CephObjectBucket, api.CephNVMeoFSubsystem}
	passed := []string{}
	for _, kind := range kinds {
		for _, object := range []string{"example", "object"} {
			for _, gatewayID := range []bool{false, true} {
				unowned := r
				unowned.Owner, unowned.FSID = "", ""
				data, _ := unowned.Canonical()
				e := executionFor(t, unowned)
				e.Block.Request, e.Block.Kind, e.Block.Object = data, string(kind), object
				if gatewayID {
					e.Block.ID = GatewayBlockID("object")
				}
				label := fmt.Sprintf("%s:%s:%t", kind, object, gatewayID)
				result := guarded(func() string {
					_, err := New(kind, nil).identity(context.Background(), e)
					if err != nil {
						return diagnostics.Of(err)[0].Message
					}
					return ""
				})
				if result != "Ceph request does not match its frozen resource identity" {
					passed = append(passed, label+"="+result)
				}
			}
		}
	}
	return strings.Join(passed, ",")
}

func dispatchLine(t *testing.T, c dispatchCase) string {
	r := c.request
	fields := []string{c.name, "arms=" + fmt.Sprint(onlyOwnArm(r))}
	for _, operation := range []string{"apply", "observe", "destroy"} {
		fields = append(fields, operation+"="+guarded(func() string {
			deadline, err := cephDeadline(r, operation)
			if err != nil {
				return "refused:" + diagnostics.Of(err)[0].Message
			}
			return deadline.String()
		}))
	}
	fields = append(fields, "subject="+guarded(func() string { return subjectOf(r, "object") }), "exclusive="+strings.Join(exclusiveFor(r.Action, r), "+"))
	var scope, ready []bool
	var effects strings.Builder
	for _, e := range dispatchEvidences(r) {
		scope = append(scope, validateScope(e, r))
		ready = append(ready, validateReady(e, r))
		for _, removal := range []bool{false, true} {
			for _, state := range []string{"ready", "absent", "partial"} {
				v := e
				v.Ready, v.Exists, v.Partial, v.Removed = state == "ready", state != "absent", state == "partial", state == "absent" && removal
				if state == "absent" {
					v.Resource, v.Client, v.Daemons, v.Endpoint = ResourceEvidence{}, "", []UnitDaemon{}, EndpointEvidence{}
				}
				data, _ := json.Marshal(v)
				effect, err := EvaluateEvidence(data, r, v.Request, removal)
				letter := map[reconciliation.EffectState]string{reconciliation.EffectNoEffect: "n", reconciliation.EffectPartial: "p", reconciliation.EffectCompleted: "c", reconciliation.EffectUnknown: "u"}[effect]
				if err != nil {
					letter = strings.ToUpper(letter)
				}
				effects.WriteString(letter)
			}
		}
	}
	fields = append(fields, "scope="+bits(scope), "ready="+bits(ready), "effects="+effects.String(), "identity="+dispatchIdentity(t, r))
	return strings.Join(fields, " ")
}

func TestActionDispatchIsPinned(t *testing.T) {
	lines := []string{}
	for _, c := range dispatchCases() {
		lines = append(lines, dispatchLine(t, c))
	}
	matchesTextGolden(t, "action-dispatch", []byte(strings.Join(lines, "\n")+"\n"))
}
