package v1alpha1

import "slices"

var definedCapabilityTokens = []string{"certManager", "dataFoundation", "kubevirt", "metallb", "nmstate"}

func DefinedCapabilityTokens() []string { return slices.Clone(definedCapabilityTokens) }

func init() {
	register(ClusterAddon, addonSchema)
	register(ClusterAddonProfile, addonProfileSchema)
	register(ClusterAddonBinding, addonBindingSchema)
}

func addonSchema() *Shape {
	s := record(
		field("provides", set(lexical("token"))),
		field("requires", set(lexical("token"))),
		field("olm", addonOLMSchema()),
		field("manifestSet", record(required("manifests", nonemptyArray(array(record(required("path", lexical("relative-path")))))))),
		field("readiness", record(defaulted("timeout", duration(), StringValue("30m")), field("checks", array(addonReadinessSchema())))),
		field("steps", named(addonStepSchema())),
	)
	s.Arms = []string{"olm", "manifestSet"}
	return s
}

func addonOLMSchema() *Shape {
	return record(
		required("namespace", record(required("name", name()), defaulted("management", enumeration("managed", "external"), StringValue("managed")), field("labels", stringsMap()))),
		field("operatorGroup", record(field("name", dns()), field("targetNamespaces", array(name())))),
		field("catalogSource", record(
			required("name", name()), required("image", image()), required("priority", integer("", "")), field("pollInterval", duration()),
			defaulted("securityContextConfig", enumeration("legacy", "restricted"), StringValue("restricted")),
		)),
		required("subscription", record(
			field("name", dns()), required("package", nonempty()), required("channel", nonempty()), required("startingCSV", nonempty()),
			required("source", dns()), defaulted("sourceNamespace", name(), StringValue("openshift-marketplace")),
		)),
		field("customResources", array(record(required("gate", nonemptyArray(array(addonReadinessSchema()))), required("resource", native())))),
	)
}

func addonReadinessSchema() *Shape {
	return choice(
		field("csvSucceeded", record(required("namespace", name()), required("subscription", dns()))),
		field("condition", record(required("apiVersion", nonempty()), required("kind", nonempty()), required("name", nonempty()), field("namespace", name()), required("condition", record(required("type", nonempty()), required("status", enumeration("True", "False", "Unknown")))))),
		field("resourceExists", record(required("apiVersion", nonempty()), required("kind", nonempty()), required("name", nonempty()), field("namespace", name()))),
	)
}

func addonStepSchema() *Shape {
	target := choice(
		field("boundCluster", record()),
		field("static", record(field("clusters", array(clusters(ContainerClusterCategory, StorageClusterCategory))), field("machines", array(ref(Machine))))),
		field("limit", enumeration("firstReachable", "all")),
	)
	target.Arms = []string{"boundCluster", "static"}
	s := record(
		required("name", lexical("token")), field("gates", enumeration("apply")), field("follows", enumeration("operatorReady", "ready")),
		field("requires", array(addonReadinessSchema())), field("source", playbookSourceSchema()),
		field("playbook", lexical("relative-path")), field("rolesPath", lexical("relative-path")), field("collectionsPath", lexical("relative-path")),
		field("target", target), field("extraVars", native()), field("secretRefs", set(ref(Secret))), field("timeout", duration()),
		field("outputs", named(record(required("name", lexical("token")), required("file", lexical("relative-path")), field("secret", boolean()), field("format", enumeration("text", "json", "sha256"))))),
		field("manifests", array(record(required("path", lexical("relative-path")), field("reclaimRendered", boolean())))),
	)
	s.Arms = []string{"gates", "follows"}
	return s
}

func addonProfileSchema() *Shape {
	return record(field("profileRefs", array(ref(ClusterAddonProfile))), field("addonRefs", array(ref(ClusterAddon))))
}

func addonBindingSchema() *Shape {
	return record(required("clusterRef", clusters(ContainerClusterCategory)), field("profileRefs", array(ref(ClusterAddonProfile))), field("addonRefs", array(ref(ClusterAddon))), field("packages", addonPackagesSchema()), field("afterRefs", set(ref(ClusterAddonBinding))))
}

type PackageSubscription struct {
	Channel         string                `json:"channel"`
	StartingCSV     string                `json:"startingCSV"`
	Source          string                `json:"source"`
	SourceNamespace string                `json:"sourceNamespace"`
	CatalogSource   *PackageCatalogSource `json:"catalogSource,omitempty"`
}
type PackageCatalogSource struct {
	Name                  string `json:"name"`
	Image                 string `json:"image"`
	Priority              int64  `json:"priority"`
	PollInterval          string `json:"pollInterval,omitempty"`
	SecurityContextConfig string `json:"securityContextConfig"`
}
type NMStateConfig struct {
	Subscription PackageSubscription `json:"subscription"`
	Timeout      string              `json:"timeout"`
}
type KubeVirtConfig struct {
	Subscription PackageSubscription `json:"subscription"`
	Timeout      string              `json:"timeout"`
}
type MetalLBConfig struct {
	Subscription PackageSubscription `json:"subscription"`
	Timeout      string              `json:"timeout"`
	Pools        []MetalLBPool       `json:"pools,omitempty"`
}
type MetalLBPool struct {
	Name       string     `json:"name"`
	Addresses  []string   `json:"addresses"`
	AutoAssign bool       `json:"autoAssign"`
	L2         *MetalLBL2 `json:"l2,omitempty"`

	ServiceAllocation *MetalLBServiceAllocation `json:"serviceAllocation,omitempty"`
}
type MetalLBServiceAllocation struct {
	Namespaces         []string          `json:"namespaces,omitempty"`
	NamespaceSelectors []MetalLBSelector `json:"namespaceSelectors,omitempty"`
	ServiceSelectors   []MetalLBSelector `json:"serviceSelectors,omitempty"`
}
type MetalLBSelector struct {
	MatchLabels map[string]string `json:"matchLabels"`
}
type MetalLBL2 struct {
	Interface string `json:"interface,omitempty"`
}

func packageSubscriptionSchema() *Shape {
	return record(required("channel", nonempty()), required("startingCSV", nonempty()), field("source", dns()), defaulted("sourceNamespace", name(), StringValue("openshift-marketplace")), field("catalogSource", record(required("name", name()), required("image", image()), required("priority", integer("", "")), field("pollInterval", duration()), defaulted("securityContextConfig", enumeration("legacy", "restricted"), StringValue("restricted")))))
}
func packageBaseSchema() *Shape {
	return record(required("subscription", packageSubscriptionSchema()), defaulted("timeout", duration(), StringValue("30m")))
}
func kubevirtPackageSchema() *Shape {
	return record(required("subscription", packageSubscriptionSchema()), defaulted("timeout", duration(), StringValue("45m")))
}
func addonPackagesSchema() *Shape {
	metallb := packageBaseSchema()
	metallb.Fields = append(metallb.Fields, field("pools", named(record(required("name", name()), required("addresses", nonemptyArray(array(nonempty()))), defaulted("autoAssign", boolean(), BoolValue(true)), field("l2", record(field("interface", nonempty()))), field("serviceAllocation", record(field("namespaces", set(name())), field("namespaceSelectors", array(record(required("matchLabels", stringsMap())))), field("serviceSelectors", array(record(required("matchLabels", stringsMap()))))))))))
	packages := record(field("nmstate", packageBaseSchema()), field("kubevirt", kubevirtPackageSchema()), field("metallb", metallb), field("fdf", fdfPackageSchema()), field("acm", acmPackageSchema()), field("gitops", gitopsPackageSchema()))
	packages.MembersSelect = true
	return packages
}
