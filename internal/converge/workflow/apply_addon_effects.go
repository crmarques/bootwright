package workflow

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionoc "github.com/crmarques/bootwright/internal/addons/oc"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

// addonEffectExecutor implements extensionoc.EffectRunner. It executes the
// add-on's compiled input effects against the bound cluster before any of the
// add-on's resources apply. The only effect today is globalPullSecretMerge:
// the binding-supplied secret becomes the password of an auths[registry] entry
// merged into openshift-config/pull-secret, so operator and operand images on
// an entitled registry (e.g. cp.icr.io) can pull.
type addonEffectExecutor struct {
	stdout, stderr io.Writer
	logPath        string
	opts           RunOptions
	plan           extensionPlanView
	inputs         []v1alpha1.ClusterAddonBindingInput
}

func newAddonEffectExecutor(stdout, stderr io.Writer, runsDir, runID string, opts RunOptions, task ApplyTask) *addonEffectExecutor {
	plan := extensionPlanView{Name: task.Extension.Name, Cluster: task.Extension.Cluster, Addon: task.Extension.Extension}
	_, inputs := addonBindingInputs(task.State, task.Extension.Binding, plan.Name)
	return &addonEffectExecutor{
		stdout:  stdout,
		stderr:  stderr,
		logPath: TaskLogPath(runsDir, runID, task.Entry.ID),
		opts:    opts,
		plan:    plan,
		inputs:  inputs,
	}
}

func (e *addonEffectExecutor) Run(ctx context.Context) error {
	for _, input := range e.plan.Addon.Spec.Accepts.Inputs {
		for _, effect := range input.Effects {
			if effect.Type != v1alpha1.ClusterAddonInputEffectGlobalPullSecretMerge {
				continue
			}
			if err := e.mergeGlobalPullSecret(ctx, input, effect); err != nil {
				return &extensionoc.EffectError{
					Effect: effect.Type,
					Input:  input.Name,
					Detail: conciseApplyTaskFailure(err.Error()),
				}
			}
		}
	}
	return nil
}

func (e *addonEffectExecutor) mergeGlobalPullSecret(ctx context.Context, input v1alpha1.ClusterAddonAcceptedInput, effect v1alpha1.ClusterAddonInputEffect) error {
	secretName := e.bindingSecretName(input)
	if secretName == "" {
		// The binding did not supply the input: the operator relies on the
		// registry credential already being in the pull secret (documented
		// prerequisite), so the effect is a declared-but-unused capability.
		return nil
	}
	store := secret.NewContextStore(effectiveContextName(e.opts.ContextName), e.opts.SecretsDir)
	password, err := store.Read(secret.MaterialKey{Name: secretName, Role: secret.MaterialPrimary})
	if err != nil {
		return fmt.Errorf("read secret %q for pull-secret merge: %w", secretName, err)
	}
	kubeconfig := clusterKubeconfigPath(e.opts.ClustersDir, e.plan.Cluster)
	// The quiet runner keeps the live pull-secret bytes off the console; they
	// land only in the sanctioned 0600 task log.
	readRunner := extensionoc.CommandRunner{LogPath: e.logPath}
	live, err := readRunner.Run(ctx, kubeconfig, []string{"get", "secret", "pull-secret", "-n", "openshift-config", "-o", "json"}, nil)
	if err != nil {
		return fmt.Errorf("read cluster pull secret: %w", err)
	}
	replacement, changed, err := mergedPullSecretReplacement(live, effect.Registry, effect.Username, string(password))
	if err != nil {
		return err
	}
	if !changed {
		fmt.Fprintf(e.stdout, "global pull secret already carries %s credentials; merge skipped\n", effect.Registry)
		return nil
	}
	runner := extensionoc.CommandRunner{LogPath: e.logPath, Stdout: e.stdout, Stderr: e.stderr}
	if _, err := runner.Run(ctx, kubeconfig, []string{"replace", "-f", "-"}, replacement); err != nil {
		return fmt.Errorf("update cluster pull secret with %s credentials: %w", effect.Registry, err)
	}
	return nil
}

// bindingSecretName resolves the value of the input's single secret-typed
// property from the binding (validation pins the schema to exactly one).
func (e *addonEffectExecutor) bindingSecretName(input v1alpha1.ClusterAddonAcceptedInput) string {
	property := ""
	for name, prop := range input.Schema.Properties {
		if prop.Secret {
			property = name
			break
		}
	}
	if property == "" {
		return ""
	}
	for _, in := range e.inputs {
		if in.Name != input.Name {
			continue
		}
		if value, ok := in.Values[property].(string); ok {
			return value
		}
	}
	return ""
}

// mergedPullSecretReplacement builds the `oc replace` payload for the global
// pull secret with auths[registry] set to the given credentials. It preserves
// the live object's resourceVersion so a concurrent writer surfaces as a
// conflict instead of being clobbered. changed is false when the live pull
// secret already carries exactly the desired entry.
func mergedPullSecretReplacement(liveSecretJSON []byte, registry, username, password string) (replacement []byte, changed bool, err error) {
	var live struct {
		Metadata struct {
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(liveSecretJSON, &live); err != nil {
		return nil, false, fmt.Errorf("decode cluster pull secret: %w", err)
	}
	encoded := live.Data[".dockerconfigjson"]
	if encoded == "" {
		return nil, false, fmt.Errorf("cluster pull secret has no .dockerconfigjson data")
	}
	config, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, false, fmt.Errorf("decode cluster pull secret payload: %w", err)
	}
	merged, changed, err := mergedDockerConfigAuth(config, registry, username, password)
	if err != nil {
		return nil, false, err
	}
	if !changed {
		return nil, false, nil
	}
	payload := map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":            "pull-secret",
			"namespace":       "openshift-config",
			"resourceVersion": live.Metadata.ResourceVersion,
		},
		"type": "kubernetes.io/dockerconfigjson",
		"data": map[string]any{
			".dockerconfigjson": base64.StdEncoding.EncodeToString(merged),
		},
	}
	replacement, err = json.Marshal(payload)
	if err != nil {
		return nil, false, fmt.Errorf("encode merged pull secret: %w", err)
	}
	return replacement, true, nil
}

// mergedDockerConfigAuth sets auths[registry] to {"auth": b64(username:password)}
// in a dockerconfigjson document, replacing any existing entry for that
// registry and leaving every other entry untouched. changed is false when the
// entry already matches.
func mergedDockerConfigAuth(config []byte, registry, username, password string) (merged []byte, changed bool, err error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(config, &doc); err != nil {
		return nil, false, fmt.Errorf("decode dockerconfigjson: %w", err)
	}
	if doc == nil {
		doc = map[string]json.RawMessage{}
	}
	auths := map[string]json.RawMessage{}
	if raw, ok := doc["auths"]; ok {
		if err := json.Unmarshal(raw, &auths); err != nil {
			return nil, false, fmt.Errorf("decode dockerconfigjson auths: %w", err)
		}
	}
	desiredAuth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	if existing, ok := auths[registry]; ok {
		var entry struct {
			Auth string `json:"auth"`
		}
		// An entry carrying the desired credential stays untouched even when it
		// has extra fields (email, username) — the registry already works.
		if err := json.Unmarshal(existing, &entry); err == nil && entry.Auth == desiredAuth {
			return nil, false, nil
		}
	}
	desired, err := json.Marshal(map[string]string{"auth": desiredAuth})
	if err != nil {
		return nil, false, err
	}
	auths[registry] = desired
	rawAuths, err := json.Marshal(auths)
	if err != nil {
		return nil, false, err
	}
	doc["auths"] = rawAuths
	merged, err = json.Marshal(doc)
	if err != nil {
		return nil, false, err
	}
	return merged, true, nil
}
