package ceph

import (
	"net/url"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
)

func packageRepositoryProxy(c api.Catalog, cluster api.Object, repository string) (string, error) {
	choice := cluster.Spec().Get("packageRepositoryProxy")
	if !choice.Has("proxyRef") {
		return "", nil
	}
	proxy, found := c.Find(api.Proxy, choice.Get("proxyRef").Text())
	if !found {
		return "", failure("api.reference", "the Ceph package Proxy is not declared", "declare spec.packageRepositoryProxy.proxyRef or select direct: {}")
	}
	connection := proxy.Spec().Get("connection")
	selectProxy, err := controller.NewProxySelector("", connection.Get("httpsProxy").Text(), choice.Get("noProxy").Strings())
	if err != nil {
		return "", failure("api.value", "the Ceph package Proxy has an invalid acquisition route", "correct spec.packageRepositoryProxy and the selected Proxy")
	}
	target, err := url.Parse(repository)
	if err != nil || target.Scheme != "https" || target.Hostname() == "" {
		return "", failure("api.value", "the Ceph package repository has no HTTPS origin", "correct spec.packageRepository")
	}
	if selected := selectProxy(target); selected != nil {
		endpoint := *selected
		endpoint.Path, endpoint.RawPath = "", ""
		return endpoint.String(), nil
	}
	return "", nil
}
