package agentinstall

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/substrate"
)

func topologyCatalog(masters, workers, infra int, ipv6 bool) api.Catalog {
	objects := largeCatalog(masters + workers + infra).Objects()
	for i, object := range objects {
		spec := object.Spec()
		if object.Kind() == api.ContainerCluster {
			nodes := spec.Get("nodes").Items()
			for j := range nodes {
				role := "master"
				if j >= masters {
					role = "worker"
				}
				if j >= masters+workers {
					role = "infra"
				}
				nodes[j] = nodes[j].With("role", api.StringValue(role))
			}
			spec = spec.With("nodes", api.ListValue(nodes...))
			if ipv6 {
				spec = spec.With("networking", api.MapValue(text("networkType", "OVNKubernetes"), field("clusterNetwork", api.ListValue(api.MapValue(text("cidr", "2001:db8:2::/48"), number("hostPrefix", "64")))), field("serviceNetwork", api.StringList("2001:db8:3::/112"))))
			}
		}
		if ipv6 {
			spec = ipv6Fixture(spec)
		}
		objects[i] = api.NewObject(object.Kind(), object.Name(), api.Value{}, spec)
	}
	return api.NewCatalog(objects)
}

func ipv6Fixture(value api.Value) api.Value {
	switch value.Type() {
	case api.String:
		text := value.Text()
		if text == "0.0.0.0/0" {
			return api.StringValue("::/0")
		}
		if strings.HasPrefix(text, "198.51.100.") {
			host, prefix, _ := strings.Cut(strings.TrimPrefix(text, "198.51.100."), "/")
			number, _ := strconv.Atoi(host)
			text = fmt.Sprintf("2001:db8:1::%x", number)
			if prefix != "" {
				text += "/64"
			}
			return api.StringValue(text)
		}
	case api.Mapping:
		fields := value.Fields()
		for i := range fields {
			fields[i].Value = ipv6Fixture(fields[i].Value)
		}
		out := api.MapValue(fields...)
		if value.Has("ipv4") {
			out = out.Without("ipv4").With("ipv6", api.MapValue(field("enabled", api.BoolValue(true)), field("dhcp", api.BoolValue(false))))
		}
		return out
	case api.Sequence:
		items := value.Items()
		for i := range items {
			items[i] = ipv6Fixture(items[i])
		}
		return api.ListValue(items...)
	}
	return value
}

func TestAnySizeProjectionGolden(t *testing.T) {
	for _, shape := range []struct {
		masters, workers, infra int
		ipv6                    bool
	}{{3, 6, 1, false}, {4, 2, 0, false}, {5, 0, 0, false}, {3, 0, 0, true}, {3, 2, 0, true}} {
		name := fmt.Sprintf("topology-%d-%d-%d-ipv6-%t", shape.masters, shape.workers, shape.infra, shape.ipv6)
		t.Run(name, func(t *testing.T) {
			catalog := topologyCatalog(shape.masters, shape.workers, shape.infra, shape.ipv6)
			media, install, _ := onlyRequests(t, catalog)
			canonical, err := media.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			matchesGolden(t, "media-request-"+name, canonical)
			if _, err := DecodeMediaRequest(canonical); err != nil {
				t.Fatal(err)
			}
			if shape.ipv6 {
				if !netip.MustParseAddr(install.Rendezvous).Is6() {
					t.Fatal("IPv6 rendezvous lost")
				}
				records := managedservice.ClusterRecords(catalog)
				if len(records) != len(install.Nodes)+3 {
					t.Fatalf("resolver records = %v", records)
				}
				for _, record := range records {
					for _, address := range record.Addresses {
						if !netip.MustParseAddr(address).Is6() {
							t.Fatalf("IPv4 fallback: %v", record)
						}
					}
				}
				config, _ := catalog.Find(api.NetworkConfig, "lab-guests")
				if gateway := substrate.DefaultGateway(config.Spec().Get("nmstate")); gateway != "2001:db8:1::1" {
					t.Fatalf("gateway=%s", gateway)
				}
			}
		})
	}
}

func TestPhysicalRoundBudgetsAndMixedKinds(t *testing.T) {
	// Four managers and eight candidate media devices bound the physical discovery.
	// Two reads, eject with certificate restoration, and disk selection cost 42 minutes per round.
	for _, test := range []struct{ nodes, boot, deadline int }{{16, 1800, 16920}, {32, 3600, 21240}, {33, 5400, 25560}} {
		budgets := installBudgets(test.nodes, substrate.ArmBaremetal)
		if budgets.FanOut != 16 || budgets.RoundMarginSeconds != 2520 || budgets.BootSeconds != test.boot || int(installDeadline(budgets, test.nodes).Seconds()) != test.deadline {
			t.Fatalf("%d physical nodes: %+v, %s", test.nodes, budgets, installDeadline(budgets, test.nodes))
		}
	}
	mixed := installBudgets(5, substrate.ArmLibvirt, substrate.ArmBaremetal)
	if mixed.FanOut != 4 || mixed.BootSeconds != 3600 || mixed.RoundMarginSeconds != 2520 {
		t.Fatalf("mixed kinds: %+v", mixed)
	}
	if largestCluster(substrate.ArmBaremetal) != 32 {
		t.Fatal("physical ceiling changed")
	}
	refused := installBudgets(29, substrate.ArmLibvirt)
	if refused.BootSeconds != 4800 || int(installDeadline(refused, 29).Seconds()) != 22680 {
		t.Fatalf("first refused emulated size: %+v", refused)
	}
}
