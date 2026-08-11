package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/converge/ansible"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/render"
)

type DestroyRunInputCounts struct {
	Renders                int64
	OwnershipLoads         int64
	SecretMaterializations int64
	KubeconfigScopes       int64
	RunnerLaunches         int64
}

type DestroyRunInputCounters struct {
	renders                atomic.Int64
	ownershipLoads         atomic.Int64
	secretMaterializations atomic.Int64
	kubeconfigScopes       atomic.Int64
	runnerLaunches         atomic.Int64
}

func (c *DestroyRunInputCounters) Counts() DestroyRunInputCounts {
	if c == nil {
		return DestroyRunInputCounts{}
	}
	return DestroyRunInputCounts{
		Renders:                c.renders.Load(),
		OwnershipLoads:         c.ownershipLoads.Load(),
		SecretMaterializations: c.secretMaterializations.Load(),
		KubeconfigScopes:       c.kubeconfigScopes.Load(),
		RunnerLaunches:         c.runnerLaunches.Load(),
	}
}

type destroyRunInputs struct {
	runsDir        string
	runID          string
	contextName    string
	contextSecrets string
	ownershipDir   string
	assetRoot      string
	preparedRender render.Result
	counters       *DestroyRunInputCounters

	ownershipMu      sync.Mutex
	ownershipValid   bool
	ownershipRecords []ownership.ResourceRecord

	renderMu sync.Mutex
	renders  map[string]*destroyRunInputRender

	secretsOnce sync.Once
	secretsErr  error
}

type destroyRunInputRender struct {
	done   chan struct{}
	result render.Result
	err    error
}

func newDestroyRunInputs(runsDir, runID string, opts RunOptions) (*destroyRunInputs, error) {
	if opts.PreparedRunRender.InventoryPath == "" || opts.PreparedRunRender.VarsPath == "" {
		return nil, fmt.Errorf("destroy graph input cache requires the immutable run render")
	}
	ownershipDir := opts.OwnershipDir
	if ownershipDir == "" {
		ownershipDir = filepath.Join(filepath.Dir(opts.ProviderStateDir), "ownership")
	}
	return &destroyRunInputs{
		runsDir:        runsDir,
		runID:          runID,
		contextName:    effectiveContextName(opts.ContextName),
		contextSecrets: opts.SecretsDir,
		ownershipDir:   ownershipDir,
		assetRoot:      opts.RenderedDir,
		preparedRender: opts.PreparedRunRender,
		counters:       opts.DestroyRunInputCounters,
		renders:        map[string]*destroyRunInputRender{},
	}, nil
}

func (c *destroyRunInputs) runtimeRoot() string {
	return filepath.Join(c.runsDir, "history", c.runID, "runtime")
}

func (c *destroyRunInputs) secretsDir() string {
	return filepath.Join(c.runtimeRoot(), runtimeSecretsDirName)
}

func (c *destroyRunInputs) close() error {
	if err := os.RemoveAll(c.runtimeRoot()); err != nil {
		return fmt.Errorf("remove destroy graph runtime secrets %s: %w", c.runtimeRoot(), err)
	}
	return nil
}

func (c *destroyRunInputs) ownershipSnapshot() ([]ownership.ResourceRecord, error) {
	c.ownershipMu.Lock()
	defer c.ownershipMu.Unlock()
	if !c.ownershipValid {
		records, err := ownership.LoadContext(c.ownershipDir, c.contextName)
		if c.counters != nil {
			c.counters.ownershipLoads.Add(1)
		}
		if err != nil {
			return nil, err
		}
		c.ownershipRecords = cloneOwnershipRecords(records)
		c.ownershipValid = true
	}
	return cloneOwnershipRecords(c.ownershipRecords), nil
}

func (c *destroyRunInputs) invalidateOwnership() {
	c.ownershipMu.Lock()
	c.ownershipValid = false
	c.ownershipMu.Unlock()
}

func (c *destroyRunInputs) prepare(opts RunOptions, records []ownership.ResourceRecord, kubeconfigPaths map[string]string) (render.Result, string, error) {
	key, err := destroyRunInputKey(opts.State, records, kubeconfigPaths)
	if err != nil {
		return render.Result{}, "", err
	}
	c.renderMu.Lock()
	entry, found := c.renders[key]
	if !found {
		entry = &destroyRunInputRender{done: make(chan struct{})}
		c.renders[key] = entry
	}
	c.renderMu.Unlock()
	if found {
		<-entry.done
		return entry.result, c.secretsDir(), entry.err
	}
	defer close(entry.done)
	if c.counters != nil {
		c.counters.renders.Add(1)
	}
	paths := render.PathOptions{
		SecretsDir:                  c.secretsDir(),
		TrustSecretsDir:             opts.SecretsDir,
		KubeVirtHostKubeconfigPaths: kubeconfigPaths,
		PreferredIdentityFile:       opts.PreferredIdentityFile,
		SSHUser:                     opts.SSHUser,
		SSHUserForProvisioned:       opts.SSHUserForProvisioned,
		AskSSHSudoPassword:          opts.SSHSudoPassword != "",
	}
	renderDir := filepath.Join(c.runsDir, "history", c.runID, "inputs", key)
	entry.result, entry.err = render.RunInputsWithAssetRoot(renderDir, c.assetRoot, paths, opts.State, records)
	if entry.err != nil {
		return entry.result, c.secretsDir(), entry.err
	}
	c.secretsOnce.Do(func() {
		if c.counters != nil {
			c.counters.secretMaterializations.Add(1)
		}
		c.secretsErr = materializeRunSecrets(c.contextName, c.contextSecrets, c.secretsDir(), true, c.preparedRender)
	})
	entry.err = c.secretsErr
	return entry.result, c.secretsDir(), entry.err
}

func destroyRunInputKey(state v1alpha1.State, records []ownership.ResourceRecord, kubeconfigPaths map[string]string) (string, error) {
	data, err := json.Marshal(struct {
		State       v1alpha1.State
		Records     []ownership.ResourceRecord
		Kubeconfigs map[string]string
	}{State: state, Records: records, Kubeconfigs: kubeconfigPaths})
	if err != nil {
		return "", fmt.Errorf("encode destroy graph input cache key: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func cloneOwnershipRecords(records []ownership.ResourceRecord) []ownership.ResourceRecord {
	out := make([]ownership.ResourceRecord, len(records))
	for i, record := range records {
		out[i] = record
		out[i].Paths = append([]string(nil), record.Paths...)
		out[i].HostFacts = cloneStringMap(record.HostFacts)
		out[i].Labels = cloneStringMap(record.Labels)
		out[i].Attributes = cloneStringMap(record.Attributes)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Context < out[j].Context
	})
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

type destroyRunInputRunner struct {
	runner   ansible.Runner
	inputs   *destroyRunInputs
	counters *DestroyRunInputCounters
}

func (r destroyRunInputRunner) Command(spec ansible.RunSpec) []string {
	return r.runner.Command(spec)
}

func (r destroyRunInputRunner) Run(ctx context.Context, spec ansible.RunSpec) error {
	if r.counters != nil {
		r.counters.runnerLaunches.Add(1)
	}
	err := r.runner.Run(ctx, spec)
	if r.inputs != nil {
		r.inputs.invalidateOwnership()
	}
	return err
}
