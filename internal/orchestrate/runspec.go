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
	Executable        string
	BundleDir         string
	StateDir          string
	SecretsDir        string
	HostStateDir      string
	InventoryPath     string
	VarsPath          string
	Playbook          string
	Limit             string
	ArtifactsDir      string
	ExtraVarPairs     []string
	Check             bool
	AskBecomePass     bool
	UseControllingTTY bool
}

func NewRunSpec(cfg RunSpecConfig) (ansible.RunSpec, error) {
	stateDirAbs, err := filepath.Abs(cfg.StateDir)
	if err != nil {
		return ansible.RunSpec{}, fmt.Errorf("resolve state dir: %w", err)
	}
	secretsDirAbs, err := filepath.Abs(cfg.SecretsDir)
	if err != nil {
		return ansible.RunSpec{}, fmt.Errorf("resolve secrets dir: %w", err)
	}
	hostStateDirAbs, err := filepath.Abs(cfg.HostStateDir)
	if err != nil {
		return ansible.RunSpec{}, fmt.Errorf("resolve host state dir: %w", err)
	}
	pairs := []string{
		"bootwright_state_dir=" + stateDirAbs,
		"bootwright_secrets_dir=" + secretsDirAbs,
		"bootwright_host_state_dir=" + hostStateDirAbs,
	}
	pairs = append(pairs, cfg.ExtraVarPairs...)
	return ansible.RunSpec{
		Executable:        cfg.Executable,
		AnsibleCfg:        filepath.Join(cfg.BundleDir, embedded.AnsibleCfgRelPath),
		RolesPath:         embedded.RolesPath(cfg.BundleDir),
		CollectionsPath:   filepath.Join(cfg.BundleDir, embedded.CollectionsRelPath),
		FilterPluginsPath: filepath.Join(cfg.BundleDir, embedded.FilterPluginsRelPath),
		Inventory:         cfg.InventoryPath,
		Playbook:          filepath.Join(cfg.BundleDir, cfg.Playbook),
		Limit:             cfg.Limit,
		ExtraVars:         cfg.VarsPath,
		ExtraVarPairs:     pairs,
		ArtifactsDir:      cfg.ArtifactsDir,
		Check:             cfg.Check,
		AskBecomePass:     cfg.AskBecomePass,
		UseControllingTTY: cfg.UseControllingTTY,
	}, nil
}

func AbsHostStateDir(hostStateDir string) (string, error) {
	hostStateDirAbs, err := filepath.Abs(hostStateDir)
	if err != nil {
		return "", fmt.Errorf("resolve host state dir: %w", err)
	}
	return hostStateDirAbs, nil
}
