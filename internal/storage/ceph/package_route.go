package ceph

import (
	"net/url"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
)

func packageRepositoryProxy(c api.Catalog, cluster api.Object, repository string) (string, error) {
	return connectProxy(c, cluster, "packageRepositoryProxy", "package", func() (*url.URL, error) {
		target, err := url.Parse(repository)
		if err != nil || target.Scheme != "https" || target.Hostname() == "" {
			return nil, failure("api.value", "the Ceph package repository has no HTTPS origin", "correct spec.packageRepository")
		}
		return target, nil
	})
}

func registryProxy(c api.Catalog, cluster api.Object, registry string) (string, error) {
	return connectProxy(c, cluster, "registryProxy", "registry", func() (*url.URL, error) {
		target, err := url.Parse("https://" + registry)
		if err != nil || target.Hostname() == "" {
			return nil, failure("api.value", "the Ceph image registry has no HTTPS authority", "correct the Entitlement registry.url")
		}
		return target, nil
	})
}

const CallHomeTarget = "https://esupport.ibm.com/connect/api/v1"

func callHomeProxy(c api.Catalog, cluster api.Object) (string, error) {
	if cluster.Spec().Get("distribution", "ibm", "callHome").Text() != "enabled" {
		return "", nil
	}
	return connectProxy(c, cluster, "distribution.ibm.callHomeProxy", "Call Home", func() (*url.URL, error) {
		return url.Parse(CallHomeTarget)
	})
}

func connectProxy(c api.Catalog, cluster api.Object, name, consumer string, origin func() (*url.URL, error)) (string, error) {
	choice := cluster.Spec().Get(strings.Split(name, ".")...)
	if !choice.Has("proxyRef") {
		return "", nil
	}
	proxy, found := c.Find(api.Proxy, choice.Get("proxyRef").Text())
	if !found {
		return "", failure("api.reference", "the Ceph "+consumer+" Proxy is not declared", "declare spec."+name+".proxyRef or select direct: {}")
	}
	connection := proxy.Spec().Get("connection")
	selectProxy, err := controller.NewProxySelector("", connection.Get("httpsProxy").Text(), choice.Get("noProxy").Strings())
	if err != nil {
		return "", failure("api.value", "the Ceph "+consumer+" Proxy has an invalid acquisition route", "correct spec."+name+" and the selected Proxy")
	}
	target, err := origin()
	if err != nil {
		return "", err
	}
	if selected := selectProxy(target); selected != nil {
		endpoint := *selected
		endpoint.Path, endpoint.RawPath = "", ""
		return endpoint.String(), nil
	}
	return "", nil
}
