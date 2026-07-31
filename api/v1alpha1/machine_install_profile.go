package v1alpha1

type MachineInstallPackageSource struct {
	Mirror           *MachineInstallPackageMirror           `yaml:"mirror,omitempty" json:"mirror,omitempty"`
	FromSubscription *MachineInstallPackageFromSubscription `yaml:"fromSubscription,omitempty" json:"fromSubscription,omitempty"`
	HostedTree       *MachineInstallPackageHostedTree       `yaml:"hostedTree,omitempty" json:"hostedTree,omitempty"`
}

func (p *MachineInstallPackageSource) GetMirror() *MachineInstallPackageMirror {
	if p == nil {
		return nil
	}
	return p.Mirror
}

func (p *MachineInstallPackageSource) GetFromSubscription() *MachineInstallPackageFromSubscription {
	if p == nil {
		return nil
	}
	return p.FromSubscription
}

func (p *MachineInstallPackageSource) GetHostedTree() *MachineInstallPackageHostedTree {
	if p == nil {
		return nil
	}
	return p.HostedTree
}

type MachineInstallPackageMirror struct {
	BaseURL      string                     `yaml:"baseURL,omitempty" json:"baseURL,omitempty"`
	Repositories []MachineInstallRepository `yaml:"repositories,omitempty" json:"repositories,omitempty"`
}

type MachineInstallPackageFromSubscription struct {
	EntitlementRef LocalObjectReference `yaml:"entitlementRef" json:"entitlementRef"`
}

type MachineInstallPackageHostedTree struct {
	FromMedia              string                    `yaml:"fromMedia" json:"fromMedia"`
	ArtifactServerEndpoint ArtifactServerEndpointRef `yaml:"artifactServerEndpoint,omitempty" json:"artifactServerEndpoint,omitempty"`
}

type MachineInstallProfile struct {
	APIVersion string                    `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                    `yaml:"kind" json:"kind"`
	Metadata   Metadata                  `yaml:"metadata" json:"metadata"`
	Spec       MachineInstallProfileSpec `yaml:"spec" json:"spec"`
	SourcePath string                    `yaml:"-" json:"-"`
}

type MachineInstallProfileSpec struct {
	OS             MachineInstallOS               `yaml:"os" json:"os"`
	Installer      MachineInstallProfileInstaller `yaml:"installer" json:"installer"`
	Subscription   *MachineOSSubscription         `yaml:"subscription,omitempty" json:"subscription,omitempty"`
	Customizations MachineInstallCustomizations   `yaml:"customizations,omitempty" json:"customizations,omitempty"`
}

type MachineOSSubscription struct {
	EntitlementRef LocalObjectReference `yaml:"entitlementRef" json:"entitlementRef"`
}

type MachineInstallOS struct {
	Family       string `yaml:"family" json:"family"`
	Version      string `yaml:"version" json:"version"`
	Architecture string `yaml:"architecture" json:"architecture"`
}

type MachineInstallProfileInstaller struct {
	Anaconda *MachineInstallAnaconda `yaml:"anaconda,omitempty" json:"anaconda,omitempty"`
}

type MachineInstallAnaconda struct {
	ImageRef            LocalObjectReference           `yaml:"imageRef" json:"imageRef"`
	RedfishVirtualMedia ArtifactServerEndpointConsumer `yaml:"redfishVirtualMedia,omitempty" json:"redfishVirtualMedia,omitempty"`
	PackageSource       *MachineInstallPackageSource   `yaml:"packageSource,omitempty" json:"packageSource,omitempty"`
}

func (a *MachineInstallAnaconda) GetPackageSource() *MachineInstallPackageSource {
	if a == nil {
		return nil
	}
	return a.PackageSource
}

type MachineInstallRepository struct {
	ID      string `yaml:"id" json:"id"`
	BaseURL string `yaml:"baseURL" json:"baseURL"`
}

type MachineInstallCustomizations struct {
	Hostname     MachineInstallHostname     `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	Localization MachineInstallLocalization `yaml:"localization,omitempty" json:"localization,omitempty"`
	SSH          MachineInstallSSH          `yaml:"ssh,omitempty" json:"ssh,omitempty"`
	Storage      MachineInstallStorage      `yaml:"storage,omitempty" json:"storage,omitempty"`
	Packages     MachineInstallPackages     `yaml:"packages,omitempty" json:"packages,omitempty"`
	Repositories MachineInstallRepositories `yaml:"repositories,omitempty" json:"repositories,omitempty"`
	Services     MachineInstallServices     `yaml:"services,omitempty" json:"services,omitempty"`
	Security     MachineInstallSecurity     `yaml:"security,omitempty" json:"security,omitempty"`
}

type MachineInstallRepositories struct {
	Configure    []MachineInstallRepositoryFile          `yaml:"configure,omitempty" json:"configure,omitempty"`
	Subscription *MachineInstallSubscriptionRepositories `yaml:"subscription,omitempty" json:"subscription,omitempty"`
}

type MachineInstallRepositoryFile struct {
	ID          string `yaml:"id" json:"id"`
	DisplayName string `yaml:"displayName,omitempty" json:"displayName,omitempty"`
	BaseURL     string `yaml:"baseURL" json:"baseURL"`
	Enabled     *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	GPGCheck    *bool  `yaml:"gpgCheck,omitempty" json:"gpgCheck,omitempty"`
	GPGKeyURL   string `yaml:"gpgKeyURL,omitempty" json:"gpgKeyURL,omitempty"`
}

type MachineInstallSubscriptionRepositories struct {
	Enable  []string `yaml:"enable,omitempty" json:"enable,omitempty"`
	Disable []string `yaml:"disable,omitempty" json:"disable,omitempty"`
}

func MachineInstallRepositoryFileEnabled(repo MachineInstallRepositoryFile) bool {
	if repo.Enabled == nil {
		return true
	}
	return *repo.Enabled
}

func MachineInstallRepositoryFileGPGCheck(repo MachineInstallRepositoryFile) bool {
	if repo.GPGCheck == nil {
		return true
	}
	return *repo.GPGCheck
}

func MachineInstallRepositoryFilePath(id string) string {
	return MachineInstallRepositoryFileDir + "/" + MachineInstallRepositoryFilePrefix + id + ".repo"
}

func MachineInstallProfileDeclaresRepositories(profile MachineInstallProfile) bool {
	repos := profile.Spec.Customizations.Repositories
	if len(repos.Configure) > 0 {
		return true
	}
	if repos.Subscription == nil {
		return false
	}
	return len(repos.Subscription.Enable) > 0 || len(repos.Subscription.Disable) > 0
}

func MachineInstallProfileRegistersSubscription(profile MachineInstallProfile) bool {
	if profile.Spec.Subscription != nil && profile.Spec.Subscription.EntitlementRef.Name != "" {
		return true
	}
	source := profile.Spec.Installer.Anaconda.GetPackageSource()
	return source.GetFromSubscription() != nil
}

type MachineInstallHostname struct {
	Source string `yaml:"source,omitempty" json:"source,omitempty"`
}

type MachineInstallLocalization struct {
	Language          string   `yaml:"language,omitempty" json:"language,omitempty"`
	Formats           string   `yaml:"formats,omitempty" json:"formats,omitempty"`
	Keyboard          string   `yaml:"keyboard,omitempty" json:"keyboard,omitempty"`
	Timezone          string   `yaml:"timezone,omitempty" json:"timezone,omitempty"`
	AdditionalLocales []string `yaml:"additionalLocales,omitempty" json:"additionalLocales,omitempty"`
}

type MachineInstallSSH struct {
	PasswordAuthentication bool                           `yaml:"passwordAuthentication,omitempty" json:"passwordAuthentication,omitempty"`
	InitialPassword        *MachineInstallInitialPassword `yaml:"initialPassword,omitempty" json:"initialPassword,omitempty"`
}

type MachineInstallInitialPassword struct {
	SecretRef SecretRef `yaml:"secretRef" json:"secretRef"`
}

type MachineInstallStorage struct {
	RootDevice MachineInstallRootDevice `yaml:"rootDevice,omitempty" json:"rootDevice,omitempty"`
}

type MachineInstallRootDevice struct {
	Source string `yaml:"source,omitempty" json:"source,omitempty"`
}

type MachineInstallPackages struct {
	Environment     string   `yaml:"environment,omitempty" json:"environment,omitempty"`
	Install         []string `yaml:"install,omitempty" json:"install,omitempty"`
	ExcludeDocs     bool     `yaml:"excludeDocs,omitempty" json:"excludeDocs,omitempty"`
	InstallWeakDeps *bool    `yaml:"installWeakDeps,omitempty" json:"installWeakDeps,omitempty"`
}

type MachineInstallServices struct {
	Enabled  []string `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Disabled []string `yaml:"disabled,omitempty" json:"disabled,omitempty"`
}

type MachineInstallSecurity struct {
	SELinux        MachineInstallSELinux         `yaml:"selinux,omitempty" json:"selinux,omitempty"`
	Firewall       MachineInstallFirewall        `yaml:"firewall,omitempty" json:"firewall,omitempty"`
	FIPS           MachineInstallFIPS            `yaml:"fips,omitempty" json:"fips,omitempty"`
	DiskEncryption *MachineInstallDiskEncryption `yaml:"diskEncryption,omitempty" json:"diskEncryption,omitempty"`
}

type MachineInstallDiskEncryption struct {
	Unlock                DiskEncryptionUnlock `yaml:"unlock,omitempty" json:"unlock,omitempty"`
	RecoveryPassphraseRef SecretRef            `yaml:"recoveryPassphraseRef,omitempty" json:"recoveryPassphraseRef,omitempty"`
}

type MachineInstallSELinux struct {
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
}

type MachineInstallFirewall struct {
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

type MachineInstallFIPS struct {
	Enabled bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}
