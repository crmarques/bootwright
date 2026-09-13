package v1alpha1

import (
	"slices"
	"testing"
)

func TestServiceKindsReplaceGenericCatalog(t *testing.T) {
	if len(Kinds()) != 26 || Schema(Kind("InfraComponent")) != nil || KindIndex(Kind("InfraComponent")) != -1 {
		t.Fatal("desired-state catalog must expose exactly 26 current kinds")
	}
	for _, kind := range []Kind{Proxy, DNSServer, NTPServer, ArtifactServer, Registry, LoadBalancer} {
		schema := Schema(kind)
		if schema == nil || schema.Open || schema.Type != Mapping {
			t.Fatalf("%s must have a closed object schema", kind)
		}
		management, ok := schema.Field("management")
		if !ok || !management.Required || !slices.Equal(management.Shape.Enums, []string{"managed", "external"}) {
			t.Fatalf("%s must require an explicit management mode", kind)
		}
		for _, field := range schema.Fields {
			if field.Default.Present() {
				t.Fatalf("%s.%s has an unconditional managed default", kind, field.Name)
			}
		}
		checkStorageShape(t, schema)
	}
	for _, legacy := range []string{"infraComponents", "proxy", "registries", "componentImages"} {
		if _, ok := Schema(Environment).Field(legacy); ok {
			t.Fatalf("legacy Environment field %s remains accepted", legacy)
		}
	}
	if _, ok := Schema(NetworkConfig).Field("nameResolutionRefs"); ok {
		t.Fatal("legacy DNS catalog selections remain accepted")
	}
	// Every managed service runs as a container, so each of the six accepts an
	// image pin and none of them selects an executable by naming one.
	for _, kind := range []Kind{Proxy, DNSServer, NTPServer, ArtifactServer, Registry, LoadBalancer} {
		if _, ok := Schema(kind).Field("image"); !ok {
			t.Fatalf("%s accepts no container image", kind)
		}
	}
	os, _ := Schema(Machine).Field("os")
	install, _ := os.Shape.Field("install")
	if _, ok := install.Shape.Field("proxy"); ok {
		t.Fatal("retired Machine installation proxy remains accepted")
	}
}

func TestServiceConsumersUseScalarTypedReferences(t *testing.T) {
	cases := []struct {
		kind   Kind
		path   []string
		target Kind
	}{
		{Environment, []string{"controller", "machineRef"}, Machine},
		{Environment, []string{"lifecycle", "rescue", "artifactServerEndpoint", "serverRef"}, ArtifactServer},
		{Machine, []string{"proxy", "proxyRef"}, Proxy},
		{Machine, []string{"os", "install", "ntp", "serverRef"}, NTPServer},
		{Machine, []string{"network", "inline", "dns", "serverRef"}, DNSServer},
		{MachineInstallProfile, []string{"proxy", "proxyRef"}, Proxy},
		{MachineInstallProfile, []string{"ntp", "serverRef"}, NTPServer},
		{NetworkConfig, []string{"dns", "serverRef"}, DNSServer},
		{ContainerCluster, []string{"install", "proxy", "proxyRef"}, Proxy},
		{ContainerCluster, []string{"install", "ntp", "serverRef"}, NTPServer},
		{ContainerCluster, []string{"install", "registries", "mirror", "registryRef"}, Registry},
		{ContainerCluster, []string{"install", "endpoints", "api", "source", "loadBalancerRef"}, LoadBalancer},
	}
	for _, tc := range cases {
		shape := Schema(tc.kind)
		for _, name := range tc.path {
			if shape.Type == Sequence {
				shape = shape.Element
			}
			field, ok := shape.Field(name)
			if !ok {
				t.Fatalf("missing consumer path %s.%v", tc.kind, tc.path)
			}
			shape = field.Shape
		}
		if shape.Type != String || !slices.Equal(shape.Reference, []Kind{tc.target}) {
			t.Fatalf("%s.%v must be a scalar reference to %s", tc.kind, tc.path, tc.target)
		}
	}
	if proxy := proxySelection(); !proxy.Atomic {
		t.Fatal("proxy selection must replace inherited choice atomically")
	}
	for _, name := range []string{"serverRef", "endpointRef"} {
		field, _ := artifactEndpoint().Field(name)
		if !field.Required {
			t.Fatalf("artifact selection %s must be explicit", name)
		}
	}
}
