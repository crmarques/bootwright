package v1alpha1

type Entitlement struct {
	APIVersion string          `yaml:"apiVersion" json:"apiVersion"`
	Kind       string          `yaml:"kind" json:"kind"`
	Metadata   Metadata        `yaml:"metadata" json:"metadata"`
	Spec       EntitlementSpec `yaml:"spec" json:"spec"`
	SourcePath string          `yaml:"-" json:"-"`
}

type EntitlementSpec struct {
	Type     string               `yaml:"type" json:"type"`
	RHSM     *EntitlementRHSM     `yaml:"rhsm,omitempty" json:"rhsm,omitempty"`
	Registry *EntitlementRegistry `yaml:"registry,omitempty" json:"registry,omitempty"`
	License  *EntitlementLicense  `yaml:"license,omitempty" json:"license,omitempty"`
}

type EntitlementRHSM struct {
	Management        string                    `yaml:"management,omitempty" json:"management,omitempty"`
	OrganizationRef   SecretRef                 `yaml:"organizationRef,omitempty" json:"organizationRef,omitempty"`
	ActivationKeyRef  SecretRef                 `yaml:"activationKeyRef,omitempty" json:"activationKeyRef,omitempty"`
	ConnectToInsights bool                      `yaml:"connectToInsights,omitempty" json:"connectToInsights,omitempty"`
	Satellite         *EntitlementRHSMSatellite `yaml:"satellite,omitempty" json:"satellite,omitempty"`
}

func EntitlementRHSMManagement(rhsm *EntitlementRHSM) string {
	if rhsm == nil || rhsm.Management == "" {
		return EntitlementRHSMManagementManaged
	}
	return rhsm.Management
}

func EntitlementByName(ents []Entitlement, name string) (Entitlement, bool) {
	if name == "" {
		return Entitlement{}, false
	}
	for _, entitlement := range ents {
		if entitlement.Metadata.Name == name {
			return entitlement, true
		}
	}
	return Entitlement{}, false
}

func EntitlementRHSMByName(ents []Entitlement, name string) *EntitlementRHSM {
	entitlement, ok := EntitlementByName(ents, name)
	if !ok {
		return nil
	}
	return entitlement.Spec.RHSM
}

type EntitlementRHSMSatellite struct {
	Hostname       string    `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	TrustBundleRef SecretRef `yaml:"trustBundleRef,omitempty" json:"trustBundleRef,omitempty"`
	ContentBaseURL string    `yaml:"contentBaseURL,omitempty" json:"contentBaseURL,omitempty"`
}

type EntitlementRegistry struct {
	URL            string    `yaml:"url,omitempty" json:"url,omitempty"`
	CredentialsRef SecretRef `yaml:"credentialsRef,omitempty" json:"credentialsRef,omitempty"`
	TrustBundleRef SecretRef `yaml:"trustBundleRef,omitempty" json:"trustBundleRef,omitempty"`
}

type EntitlementLicense struct {
	Accept bool `yaml:"accept,omitempty" json:"accept,omitempty"`
}

func EntitlementTypes() []string {
	return []string{
		EntitlementTypeRedHatRHEL,
		EntitlementTypeRedHatCeph,
		EntitlementTypeIBMStorageCeph,
	}
}

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
