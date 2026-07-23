package workflow

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionoc "github.com/crmarques/bootwright/internal/addons/oc"
	secret "github.com/crmarques/bootwright/internal/secrets"
)

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
			if effect.GlobalPullSecretMerge == nil {
				continue
			}
			if err := e.mergeGlobalPullSecret(ctx, input, *effect.GlobalPullSecretMerge); err != nil {
				return &extensionoc.EffectError{
					Effect: v1alpha1.ClusterAddonInputEffectGlobalPullSecretMerge,
					Input:  input.Name,
					Detail: conciseApplyTaskFailure(err.Error()),
				}
			}
		}
	}
	return nil
}

func (e *addonEffectExecutor) mergeGlobalPullSecret(ctx context.Context, input v1alpha1.ClusterAddonAcceptedInput, effect v1alpha1.ClusterAddonGlobalPullSecretMergeEffect) error {
	secretName := e.bindingSecretName(input)
	if secretName == "" {
		return nil
	}
	store := secret.NewContextStore(effectiveContextName(e.opts.ContextName), e.opts.SecretsDir)
	password, err := store.Read(secret.MaterialKey{Name: secretName, Role: secret.MaterialPrimary})
	if err != nil {
		return fmt.Errorf("read secret %q for pull-secret merge: %w", secretName, err)
	}
	return withMaterializedClusterKubeconfig(e.opts.ContextName, e.opts.ClustersDir, e.plan.Cluster, func(kubeconfig string) error {
		runner := extensionoc.CommandRunner{LogPath: e.logPath, RedactLog: true, Stdout: e.stdout, Stderr: e.stderr}
		changed, err := mergeGlobalPullSecretCredential(ctx, runner, kubeconfig, effect.Registry, effect.Username, string(password))
		if err != nil {
			return err
		}
		if !changed {
			fmt.Fprintf(e.stdout, "global pull secret already carries %s credentials; merge skipped\n", effect.Registry)
		}
		return nil
	})
}

const pullSecretMergeAttempts = 3

func mergeGlobalPullSecretCredential(ctx context.Context, runner extensionoc.OCRunner, kubeconfig, registry, username, password string) (bool, error) {
	var conflict error
	for attempt := 0; attempt < pullSecretMergeAttempts; attempt++ {
		live, err := runner.Run(ctx, kubeconfig, []string{"get", "secret", "pull-secret", "-n", "openshift-config", "-o", "json"}, nil)
		if err != nil {
			return false, fmt.Errorf("read cluster pull secret: %w", err)
		}
		replacement, changed, err := mergedPullSecretReplacement(live, registry, username, password)
		if err != nil {
			return false, err
		}
		if !changed {
			return false, nil
		}
		if _, err := runner.Run(ctx, kubeconfig, []string{"replace", "-f", "-"}, replacement); err != nil {
			if pullSecretUpdateConflict(err) {
				conflict = err
				continue
			}
			return false, fmt.Errorf("update cluster pull secret with %s credentials: %w", registry, err)
		}
		return true, nil
	}
	return false, fmt.Errorf("update cluster pull secret with %s credentials after %d attempts: %w", registry, pullSecretMergeAttempts, conflict)
}

func pullSecretUpdateConflict(err error) bool {
	message := err.Error()
	return strings.Contains(message, "Error from server (Conflict)") ||
		strings.Contains(message, "the object has been modified")
}

func (e *addonEffectExecutor) bindingSecretName(input v1alpha1.ClusterAddonAcceptedInput) string {
	if input.SecretRef == nil {
		return ""
	}
	for _, in := range e.inputs {
		if in.Name != input.Name {
			continue
		}
		return in.Value
	}
	return ""
}

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
