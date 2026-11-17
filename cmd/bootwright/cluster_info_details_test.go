//go:build linux && amd64

package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
)

func TestClusterInfoIncludesOpenShiftURLsAndTopologyBeforeInstallation(t *testing.T) {
	t.Parallel()
	services, _, _ := snoContext(t)
	out, stderr := contextRun(t, services, 0, "cluster", "info", "--output", "json")
	var envelope struct {
		Result struct {
			Clusters []struct {
				Name, Distribution, Release, Platform, InstallMode, BaseDomain, Status string
				Endpoints                                                              []struct{ Service, URL, Address, Source string }
				Nodes                                                                  []struct{ Name, Role, Machine, FQDN, Address string }
				Networking                                                             struct {
					Type                             string
					MachineNetworks, ServiceNetworks []string
					ClusterNetworks                  []struct {
						CIDR       string
						HostPrefix int
					}
				}
			}
		}
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil || stderr != "" || len(envelope.Result.Clusters) != 1 {
		t.Fatalf("cluster info = %s, stderr %q, error %v", out, stderr, err)
	}
	cluster := envelope.Result.Clusters[0]
	if cluster.Distribution != "openshift" || cluster.Release != "4.21.15" || cluster.Platform != "none" || cluster.InstallMode != "connected" ||
		cluster.BaseDomain != "lab.example.test" || cluster.Status != "unavailable" {
		t.Fatalf("cluster settings = %+v", cluster)
	}
	if len(cluster.Endpoints) != 3 || cluster.Endpoints[0].Service != "api" || cluster.Endpoints[2].Service != "console" ||
		cluster.Endpoints[0].URL != "https://api."+cluster.Name+".lab.example.test:6443" ||
		cluster.Endpoints[2].URL != "https://console-openshift-console.apps."+cluster.Name+".lab.example.test" {
		t.Fatalf("endpoints = %+v", cluster.Endpoints)
	}
	if len(cluster.Nodes) != 1 || cluster.Nodes[0].Role != "controlPlane" || cluster.Nodes[0].Machine == "" || cluster.Nodes[0].FQDN == "" ||
		cluster.Nodes[0].Address == "" || cluster.Nodes[0].Address != cluster.Endpoints[0].Address || cluster.Endpoints[0].Source != "node" {
		t.Fatalf("node and endpoint = %+v, %+v", cluster.Nodes, cluster.Endpoints)
	}
	if cluster.Networking.Type != "" || len(cluster.Networking.MachineNetworks) != 1 || len(cluster.Networking.ClusterNetworks) != 1 || len(cluster.Networking.ServiceNetworks) != 1 {
		t.Fatalf("networking = %+v", cluster.Networking)
	}
	out, stderr = contextRun(t, services, 0, "cluster", "info", "--name", cluster.Name)
	for _, want := range []string{cluster.Endpoints[0].URL, cluster.Endpoints[2].URL, cluster.Nodes[0].FQDN, "Installation", "recorded locally", "installer default"} {
		if stderr != "" || !strings.Contains(out, want) {
			t.Fatalf("default text lacks %q: %s, stderr %q", want, out, stderr)
		}
	}
}

func TestClusterInfoConsolePasswordIsExplicitAndPrefersUpdatedCustody(t *testing.T) {
	t.Parallel()
	services, repository, _ := snoContext(t)
	const initial = "synthetic-initial-console-password"
	const rotated = "synthetic-rotated-console-password"
	for _, copy := range []struct{ name, password string }{{agentinstall.ConsolePasswordOutput, initial}, {agentinstall.AdminPasswordOutput, rotated}} {
		produceThroughLentArea(t, services, repository, "lab-sno", agentinstall.InstallBlockID("sno"), copy.name, copy.password)
		ordinary, stderr := contextRun(t, services, 0, "cluster", "info", "--name", "sno", "--output", "json")
		if stderr != "" || strings.Contains(ordinary, initial) || strings.Contains(ordinary, rotated) {
			t.Fatalf("ordinary info disclosed material: %s %s", ordinary, stderr)
		}
		for _, format := range []string{"text", "json", "yaml"} {
			out, stderr := contextRun(t, services, 0, "cluster", "info", "--name", "sno", "--secrets", "--output", format)
			if stderr != "" || !strings.Contains(out, "kubeadmin") || !strings.Contains(out, copy.password) {
				t.Fatalf("%s credential output = %s, stderr %s", format, out, stderr)
			}
			if copy.password == rotated && strings.Contains(out, initial) {
				t.Fatal("rotated custody was replaced by the installer's original password")
			}
		}
	}
}
