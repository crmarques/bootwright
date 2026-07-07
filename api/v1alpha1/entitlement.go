package v1alpha1

// Entitlement is named vendor-controlled content access for one product. It is
// referenced BY NAME from StorageCluster.spec.ceph.entitlementRef,
// MachineImage.spec.packageSource.redhatCDN.entitlementRef, and from another
// Entitlement via spec.rhelEntitlementRef. Secrets it names live on
// Environment.spec.secrets and are resolved by name (never by value).
//
// spec.type is the discriminator (a discriminated union on the API's dominant
// grammar). The required arms follow from the type; provider and product are
// derived from it at resolve time (see EntitlementTypeProviderProduct) so the
// day-2 cephadm render sees the same provider/product strings as before:
//
//	type              required arms
//	redhat-rhel       rhsm
//	redhat-ceph       rhsm + registry.credentialsRef   (one Red Hat subscription covers RHEL + the rhceph repo)
//	ibm-storage-ceph  registry.credentialsRef + license.accept:true + rhelEntitlementRef
//
// IBM Storage Ceph ships its own image registry (cp.icr.io) and product license
// but runs on RHEL it does not itself entitle: its packages come from a public
// IBM repo, while the RHEL BaseOS/AppStream repos cephadm needs are reached
// through a separate Red Hat subscription. So an ibm-storage-ceph entitlement
// carries registry + license and names a redhat-rhel entitlement via
// rhelEntitlementRef for that subscription; an inline rhsm arm on it is
// rejected. (redhat-ceph stays bundled: a single Red Hat subscription entitles
// both RHEL and the rhceph tools repo, so its own rhsm arm covers both.)
type Entitlement struct {
	APIVersion string          `yaml:"apiVersion" json:"apiVersion"`
	Kind       string          `yaml:"kind" json:"kind"`
	Metadata   Metadata        `yaml:"metadata" json:"metadata"`
	Spec       EntitlementSpec `yaml:"spec" json:"spec"`
	SourcePath string          `yaml:"-" json:"-"`
}

type EntitlementSpec struct {
	// Type is the discriminator: redhat-rhel | redhat-ceph | ibm-storage-ceph.
	Type string `yaml:"type" json:"type"`
	// RHELEntitlementRef names a redhat-rhel Entitlement supplying the RHEL
	// subscription (rhsm) this entitlement depends on but does not itself
	// carry. Required for ibm-storage-ceph (which takes no inline rhsm arm);
	// rejected on every other type.
	RHELEntitlementRef LocalObjectReference `yaml:"rhelEntitlementRef,omitempty" json:"rhelEntitlementRef,omitempty"`
	RHSM               *EntitlementRHSM     `yaml:"rhsm,omitempty" json:"rhsm,omitempty"`
	Registry           *EntitlementRegistry `yaml:"registry,omitempty" json:"registry,omitempty"`
	License            *EntitlementLicense  `yaml:"license,omitempty" json:"license,omitempty"`
}

type EntitlementRHSM struct {
	OrganizationRef   SecretRef                 `yaml:"organizationRef,omitempty" json:"organizationRef,omitempty"`
	ActivationKeyRef  SecretRef                 `yaml:"activationKeyRef,omitempty" json:"activationKeyRef,omitempty"`
	ConnectToInsights bool                      `yaml:"connectToInsights,omitempty" json:"connectToInsights,omitempty"`
	Satellite         *EntitlementRHSMSatellite `yaml:"satellite,omitempty" json:"satellite,omitempty"`
}

// EntitlementRHSMSatellite redirects this entitlement's RHSM registration from
// the public Red Hat CDN (subscription.redhat.io) to a corporate Red Hat
// Satellite or Capsule. When set, both the install-time Anaconda kickstart
// (rhsm --server-hostname / --rhsm-baseurl) and the day-2 cephadm
// subscription-manager register target this server; the org and activation key
// on the enclosing rhsm arm are interpreted against it. The CA bundle named by
// trustBundleRef is trusted (anchors + update-ca-trust) before registration so
// the Satellite's certificate validates. Applies to RHEL/Anaconda installs; the
// rhsm kickstart command is RHEL-only.
type EntitlementRHSMSatellite struct {
	// Hostname is the Satellite or Capsule FQDN, e.g.
	// satellite.corp.example.com. It is a bare host (no scheme or path); the
	// renderer derives the rhsm server-hostname and, when ContentBaseURL is
	// unset, the default content baseurl from it.
	Hostname string `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	// TrustBundleRef names a spec.secrets PEM CA bundle for the Satellite's
	// certificate chain, mirroring registry.trustBundleRef. Required in
	// practice for the common private/self-signed Satellite CA; omit only when
	// the Satellite certificate already chains to an already-trusted root.
	TrustBundleRef SecretRef `yaml:"trustBundleRef,omitempty" json:"trustBundleRef,omitempty"`
	// ContentBaseURL overrides the subscription content host (rhsm baseurl).
	// Defaults during normalization to https://<hostname>/pulp/content. Set it
	// for non-standard content paths or a Capsule.
	ContentBaseURL string `yaml:"contentBaseURL,omitempty" json:"contentBaseURL,omitempty"`
}

type EntitlementRegistry struct {
	URL            string    `yaml:"url,omitempty" json:"url,omitempty"`
	CredentialsRef SecretRef `yaml:"credentialsRef,omitempty" json:"credentialsRef,omitempty"`
	TrustBundleRef SecretRef `yaml:"trustBundleRef,omitempty" json:"trustBundleRef,omitempty"`
}

type EntitlementLicense struct {
	Accept bool `yaml:"accept,omitempty" json:"accept,omitempty"`
}

// EntitlementTypes lists the valid spec.type values in canonical order.
func EntitlementTypes() []string {
	return []string{
		EntitlementTypeRedHatRHEL,
		EntitlementTypeRedHatCeph,
		EntitlementTypeIBMStorageCeph,
	}
}

// EntitlementTypeProviderProduct maps a spec.type to the (provider, product)
// spellings the resolver emits into the cephadm ansible vars, preserving the
// day-2 render contract after the provider/product pair collapsed into type.
func EntitlementTypeProviderProduct(entitlementType string) (provider, product string, ok bool) {
	switch entitlementType {
	case EntitlementTypeRedHatRHEL:
		return EntitlementProviderRedHat, EntitlementProductRHEL, true
	case EntitlementTypeRedHatCeph:
		return EntitlementProviderRedHat, EntitlementProductCeph, true
	case EntitlementTypeIBMStorageCeph:
		return EntitlementProviderIBM, EntitlementProductIBMStorageCeph, true
	default:
		return "", "", false
	}
}
