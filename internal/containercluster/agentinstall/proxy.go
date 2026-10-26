package agentinstall

import (
	"net/netip"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	environmentdomain "github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/substrate"
)

// InstallProxy freezes endpoints with an explicit authentication placeholder.
// Only the attempt substitutes the bound username and password.
type InstallProxy struct {
	CredentialsRef string `json:"credentialsRef,omitempty"`
	HTTPProxy      string `json:"httpProxy"`
	HTTPSProxy     string `json:"httpsProxy"`
	NoProxy        string `json:"noProxy"`
}

func installationProxy(catalog api.Catalog, cluster api.Object) (*InstallProxy, string, error) {
	choice := cluster.Spec().Get("install", "proxy")
	if !choice.Present() || choice.Has("direct") {
		return nil, "", nil
	}
	proxy, found := catalog.Find(api.Proxy, choice.Get("proxyRef").Text())
	if !found {
		return nil, "", refusal("api.reference", "the selected installation Proxy is absent", "correct install.proxy on "+cluster.Identity())
	}
	result := &InstallProxy{}
	managed := ""
	if proxy.Spec().Get("management").Text() == "managed" {
		address, required, err := managedservice.ServiceEndpoint(catalog, api.Proxy, choice.With("serverRef", choice.Get("proxyRef")), cluster.Identity())
		if err != nil {
			return nil, "", err
		}
		result.HTTPProxy = "http://" + managedservice.HostPort(address, 3128)
		result.HTTPSProxy, managed = result.HTTPProxy, required
	} else {
		connection := proxy.Spec().Get("connection")
		if trust := connection.Get("trustBundleRef").Text(); trust != "" && !slices.Contains(cluster.Spec().Get("install", "additionalTrustBundleRefs").Strings(), trust) {
			return nil, "", refusal("api.invariant", "the installation proxy trust bundle is not selected by the cluster", "add Secret "+trust+" to install.additionalTrustBundleRefs on "+cluster.Identity())
		}
		result.HTTPProxy, result.HTTPSProxy = connection.Get("httpProxy").Text(), connection.Get("httpsProxy").Text()
		if result.HTTPProxy == "" {
			result.HTTPProxy = result.HTTPSProxy
		}
		if result.HTTPSProxy == "" {
			result.HTTPSProxy = result.HTTPProxy
		}
		if result.HTTPProxy == "" {
			return nil, "", refusal("api.required", "the installation proxy declares no endpoint", "correct the selected Proxy connection")
		}
		result.CredentialsRef = connection.Get("auth", "proxyAuthRef").Text()
	}
	entries := []string{}
	for _, entry := range choice.Get("noProxy").Strings() {
		translated, err := installerBypass(entry)
		if err != nil {
			return nil, "", err
		}
		entries = append(entries, translated)
	}
	for _, node := range cluster.Spec().Get("nodes").Items() {
		machine, ok := catalog.Find(api.Machine, node.Get("machineRef").Text())
		if !ok {
			continue
		}
		network, err := substrate.NetworkSpec(catalog, machine)
		if err != nil {
			return nil, "", err
		}
		for _, item := range network.Get("machineNetwork").Items() {
			cidr := item.Get("cidr").Text()
			if !slices.Contains(entries, cidr) {
				entries = append(entries, cidr)
			}
		}
	}
	entries = append(entries, "."+cluster.Name()+"."+environmentdomain.ContainerClusterDomain(catalog))
	result.NoProxy = strings.Join(entries, ",")
	return result, managed, nil
}

func installerBypass(entry string) (string, error) {
	if entry == "*" {
		return entry, nil
	}
	unbracketed := strings.TrimSuffix(strings.TrimPrefix(entry, "["), "]")
	if address, err := netip.ParseAddr(unbracketed); err == nil {
		return address.String(), nil
	}
	if prefix, err := netip.ParsePrefix(entry); err == nil {
		return prefix.String(), nil
	}
	if strings.Contains(entry, ":") {
		return "", refusal("api.value", "the installer proxy bypass grammar does not accept host ports", "remove the port from install.proxy.noProxy")
	}
	return strings.TrimPrefix(entry, "*"), nil
}
