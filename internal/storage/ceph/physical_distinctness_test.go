package ceph

import (
	"encoding/json"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func TestObservedPhysicalDisksAreDisjointAcrossSelectors(t *testing.T) {
	for _, collision := range []string{"none", "serial", "wwn", "missing"} {
		t.Run(collision, func(t *testing.T) {
			_, e, r, _ := physicalRetirementFixture(t)
			r.Owner, r.FSID, r.Image = e.ApplyOperation, FSIDFor(e.ApplyOperation, r.Cluster), PinnedRepository+"@"+resolvedDigest
			proofs := []lifecycle.BlockEvidence{}
			for _, pair := range e.RemovalProved {
				if pair.Applied.Implementation == HostImplementation {
					proofs = append(proofs, pair.Applied)
				}
			}
			first, err := decodeHostEvidence(proofs[0].Evidence, proofs[0].Block.RequestDigest)
			if err != nil {
				t.Fatal(err)
			}
			if collision == "serial" || collision == "wwn" {
				second, _ := decodeHostEvidence(proofs[1].Evidence, proofs[1].Block.RequestDigest)
				if collision == "serial" {
					second.Devices[0].Serial = first.Devices[0].Serial
				} else {
					observed := &second.Devices[0]
					observed.Path = "/dev/disk/by-serial/" + observed.Serial
					observed.Alias = "/dev/disk/by-id/SYNTHETIC_" + observed.Serial
					observed.WWN = first.Devices[0].WWN
					r.Hosts[1].Devices = []Device{{Path: observed.Path, Serial: observed.Serial, SizeBytes: observed.SizeBytes}}
					prepared, _ := DecodeHostRequest(proofs[1].Block.Request)
					for i := range prepared.Cluster.Hosts {
						if prepared.Cluster.Hosts[i].Name == r.Hosts[1].Name {
							prepared.Cluster.Hosts[i].Devices = r.Hosts[1].Devices
						}
					}
					proofs[1].Block.Request, _ = prepared.canonical()
				}
				proofs[1].Evidence, _ = json.Marshal(second)
			}
			if collision == "missing" {
				proofs = proofs[:1]
			}
			e.ApplyProved = proofs
			err = proveDistinctHostDevices(e, r)
			if collision == "none" && err != nil || collision != "none" && err == nil {
				t.Fatalf("%s: %v", collision, err)
			}
		})
	}
}
