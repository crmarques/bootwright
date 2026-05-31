// Package runconfig is the bridge from the renderer output to the
// Ansible runner. It builds an ansible.RunSpec from the rendered
// inventory + vars paths and the embedded bundle layout; nothing here
// touches state, validation, or installer assets — that's render's job.
package runconfig

import (
	"fmt"
	"path/filepath"

	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/converge/bundle"
)

type RunSpecConfig struct {
	Executable         string
	BundleDir          string
	RenderedDir        string
	ClustersDir        string
	RunsDir            string
	SecretsDir         string
	ManagedServicesDir string
	ProviderStateDir   string
	InventoryPath      string
	VarsPath           string
	Playbook           string
	Limit              string
	Forks              int
	ArtifactsDir       string
	OutputLogPath      string
	ExtraVarPairs      []string
	Check              bool
	AskBecomePass      bool
	BecomePasswordFile string
	UseControllingTTY  bool
}

func NewRunSpec(cfg RunSpecConfig) (ansible.RunSpec, error) {
	renderedDirAbs, err := filepath.Abs(cfg.RenderedDir)
	if err != nil {
		return ansible.RunSpec{}, fmt.Errorf("resolve rendered dir: %w", err)
	}
	clustersDirAbs, err := filepath.Abs(cfg.ClustersDir)
	if err != nil {
		return ansible.RunSpec{}, fmt.Errorf("resolve clusters dir: %w", err)
	}
	runsDirAbs, err := filepath.Abs(cfg.RunsDir)
	if err != nil {
		return ansible.RunSpec{}, fmt.Errorf("resolve runs dir: %w", err)
	}
	secretsDirAbs, err := filepath.Abs(cfg.SecretsDir)
	if err != nil {
		return ansible.RunSpec{}, fmt.Errorf("resolve secrets dir: %w", err)
	}
	managedServicesDirAbs, err := filepath.Abs(cfg.ManagedServicesDir)
	if err != nil {
		return ansible.RunSpec{}, fmt.Errorf("resolve managed services dir: %w", err)
	}
	providerStateDirAbs, err := filepath.Abs(cfg.ProviderStateDir)
	if err != nil {
		return ansible.RunSpec{}, fmt.Errorf("resolve provider state dir: %w", err)
	}
	artifactsDirAbs, err := filepath.Abs(cfg.ArtifactsDir)
	if err != nil {
		return ansible.RunSpec{}, fmt.Errorf("resolve Ansible artifacts dir: %w", err)
	}
	pairs := []string{
		"bootwright_rendered_dir=" + renderedDirAbs,
		"bootwright_clusters_dir=" + clustersDirAbs,
		"bootwright_runs_dir=" + runsDirAbs,
		"bootwright_secrets_dir=" + secretsDirAbs,
		"bootwright_managed_services_dir=" + managedServicesDirAbs,
		"bootwright_provider_state_dir=" + providerStateDirAbs,
		"bootwright_ansible_artifacts_dir=" + artifactsDirAbs,
	}
	pairs = append(pairs, cfg.ExtraVarPairs...)
	return ansible.RunSpec{
		Executable:         cfg.Executable,
		AnsibleCfg:         filepath.Join(cfg.BundleDir, bundle.AnsibleCfgRelPath),
		RolesPath:          bundle.RolesPath(cfg.BundleDir),
		CollectionsPath:    filepath.Join(cfg.BundleDir, bundle.CollectionsRelPath),
		FilterPluginsPath:  filepath.Join(cfg.BundleDir, bundle.FilterPluginsRelPath),
		Inventory:          cfg.InventoryPath,
		Playbook:           filepath.Join(cfg.BundleDir, cfg.Playbook),
		Limit:              cfg.Limit,
		Forks:              cfg.Forks,
		ExtraVars:          cfg.VarsPath,
		ExtraVarPairs:      pairs,
		ArtifactsDir:       artifactsDirAbs,
		OutputLogPath:      cfg.OutputLogPath,
		Check:              cfg.Check,
		AskBecomePass:      cfg.AskBecomePass,
		BecomePasswordFile: cfg.BecomePasswordFile,
		UseControllingTTY:  cfg.UseControllingTTY,
	}, nil
}
