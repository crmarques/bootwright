// Package orchestrate is the bridge from the renderer output to the
// Ansible runner. It builds an ansible.RunSpec from the rendered
// inventory + vars paths and the embedded bundle layout; nothing here
// touches state, validation, or installer assets — that's render's job.
package orchestrate

import (
	"fmt"
	"path/filepath"

	"github.com/crmarques/bootwright/internal/ansible"
	"github.com/crmarques/bootwright/internal/embedded"
)

type RunSpecConfig struct {
	Executable         string
	BundleDir          string
	RenderedDir        string
	RuntimeDir         string
	RunsDir            string
	SecretsDir         string
	ManagedDir         string
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
	runtimeDirAbs, err := filepath.Abs(cfg.RuntimeDir)
	if err != nil {
		return ansible.RunSpec{}, fmt.Errorf("resolve runtime dir: %w", err)
	}
	runsDirAbs, err := filepath.Abs(cfg.RunsDir)
	if err != nil {
		return ansible.RunSpec{}, fmt.Errorf("resolve runs dir: %w", err)
	}
	secretsDirAbs, err := filepath.Abs(cfg.SecretsDir)
	if err != nil {
		return ansible.RunSpec{}, fmt.Errorf("resolve secrets dir: %w", err)
	}
	managedDirAbs, err := filepath.Abs(cfg.ManagedDir)
	if err != nil {
		return ansible.RunSpec{}, fmt.Errorf("resolve managed dir: %w", err)
	}
	pairs := []string{
		"bootwright_rendered_dir=" + renderedDirAbs,
		"bootwright_runtime_dir=" + runtimeDirAbs,
		"bootwright_runs_dir=" + runsDirAbs,
		"bootwright_secrets_dir=" + secretsDirAbs,
		"bootwright_managed_dir=" + managedDirAbs,
	}
	pairs = append(pairs, cfg.ExtraVarPairs...)
	return ansible.RunSpec{
		Executable:         cfg.Executable,
		AnsibleCfg:         filepath.Join(cfg.BundleDir, embedded.AnsibleCfgRelPath),
		RolesPath:          embedded.RolesPath(cfg.BundleDir),
		CollectionsPath:    filepath.Join(cfg.BundleDir, embedded.CollectionsRelPath),
		FilterPluginsPath:  filepath.Join(cfg.BundleDir, embedded.FilterPluginsRelPath),
		Inventory:          cfg.InventoryPath,
		Playbook:           filepath.Join(cfg.BundleDir, cfg.Playbook),
		Limit:              cfg.Limit,
		Forks:              cfg.Forks,
		ExtraVars:          cfg.VarsPath,
		ExtraVarPairs:      pairs,
		ArtifactsDir:       cfg.ArtifactsDir,
		OutputLogPath:      cfg.OutputLogPath,
		Check:              cfg.Check,
		AskBecomePass:      cfg.AskBecomePass,
		BecomePasswordFile: cfg.BecomePasswordFile,
		UseControllingTTY:  cfg.UseControllingTTY,
	}, nil
}
