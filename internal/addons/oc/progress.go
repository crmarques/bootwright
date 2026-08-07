package oc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

type waitProgress struct {
	w         io.Writer
	heartbeat time.Duration
	last      string
	lastAt    time.Time
	emitted   bool
}

func startWaitProgress(w io.Writer) *waitProgress {
	return &waitProgress{w: w, heartbeat: 30 * time.Second}
}

func (p *waitProgress) observe(detail string) {
	if p == nil || p.w == nil || strings.TrimSpace(detail) == "" {
		return
	}
	now := time.Now()
	if detail == p.last && p.emitted && now.Sub(p.lastAt) < p.heartbeat {
		return
	}
	p.last = detail
	p.lastAt = now
	p.emitted = true
	p.line(detail)
}

func (p *waitProgress) done(detail string) {
	p.observe(detail)
}

func (p *waitProgress) line(msg string) {
	if p == nil || p.w == nil {
		return
	}
	fmt.Fprintln(p.w, msg)
}

const diagnosisBudget = 30 * time.Second

func diagnoseChecks(ctx context.Context, runner OCRunner, kubeconfig string, checks []v1alpha1.ClusterAddonReadinessCheck, p *waitProgress) string {
	var cause string
	for _, check := range checks {
		var observed string
		switch {
		case check.CSVSucceeded != nil:
			observed = diagnoseCSVGate(ctx, runner, kubeconfig, check.CSVSucceeded.Namespace, check.CSVSucceeded.Subscription, p)
		case check.Condition != nil:
			condition := check.Condition
			observed = diagnoseObject(ctx, runner, kubeconfig, condition.APIVersion, condition.Kind, condition.Namespace, condition.Name, p)
		case check.ResourceExists != nil:
			exists := check.ResourceExists
			observed = diagnoseObject(ctx, runner, kubeconfig, exists.APIVersion, exists.Kind, exists.Namespace, exists.Name, p)
		}
		if observed != "" && cause == "" {
			cause = observed
		}
	}
	return clip(strings.TrimSpace(cause), 400)
}

func diagnoseObject(ctx context.Context, runner OCRunner, kubeconfig, apiVersion, kind, namespace, name string, p *waitProgress) string {
	resource := resourceArg(apiVersion, kind) + "/" + name
	obj, err := getResource(ctx, runner, kubeconfig, apiVersion, kind, namespace, name)
	if err != nil {
		p.line(fmt.Sprintf("  %s could not be read: %v", resource, err))
		return resource + " could not be read"
	}
	if phase := nestedString(obj, "status", "phase"); phase != "" {
		p.line(fmt.Sprintf("  %s phase=%s", resource, phase))
	}
	own := conditionFindings(obj, kind, namespace, name, p)
	related := diagnoseRelatedObjects(ctx, runner, kubeconfig, obj, p)
	return bestFinding(own, related)
}

const relatedObjectLimit = 6

func diagnoseRelatedObjects(ctx context.Context, runner OCRunner, kubeconfig string, obj map[string]any, p *waitProgress) []conditionFinding {
	items, _ := nestedValue(obj, "status", "relatedObjects").([]any)
	var findings []conditionFinding
	read := 0
	for index, item := range items {
		ref, ok := item.(map[string]any)
		if !ok {
			continue
		}
		apiVersion := stringValue(ref["apiVersion"])
		kind := stringValue(ref["kind"])
		name := stringValue(ref["name"])
		namespace := stringValue(ref["namespace"])
		if apiVersion == "" || kind == "" || name == "" {
			continue
		}
		if read >= relatedObjectLimit {
			p.line(fmt.Sprintf("  %d further related objects not read", len(items)-index))
			break
		}
		read++
		resource := resourceArg(apiVersion, kind) + "/" + name
		related, err := getResource(ctx, runner, kubeconfig, apiVersion, kind, namespace, name)
		if err != nil {
			p.line(fmt.Sprintf("  related %s could not be read: %v", resource, err))
			continue
		}
		p.line(fmt.Sprintf("  related %s phase=%s", resource, orNone(nestedString(related, "status", "phase"))))
		findings = append(findings, conditionFindings(related, kind, namespace, name, p)...)
	}
	return findings
}

func diagnoseCSVGate(ctx context.Context, runner OCRunner, kubeconfig, namespace, subscription string, p *waitProgress) string {
	p.line(fmt.Sprintf("diagnosing why the operator CSV for Subscription/%s/%s has not reached Succeeded:", namespace, subscription))
	var cause string
	sub, err := getNamedResource(ctx, runner, kubeconfig, "subscription.operators.coreos.com", namespace, subscription)
	if err != nil {
		p.line(fmt.Sprintf("  Subscription/%s/%s could not be read: %v", namespace, subscription, err))
		return ""
	}
	if c := formatConditions(sub, "Subscription", namespace, subscription, p); c != "" {
		cause = c
	}
	installed := nestedString(sub, "status", "installedCSV")
	if plan := nestedString(sub, "status", "installPlanRef", "name"); plan != "" {
		if ip, ipErr := getNamedResource(ctx, runner, kubeconfig, "installplan.operators.coreos.com", namespace, plan); ipErr == nil {
			p.line(fmt.Sprintf("  InstallPlan/%s/%s phase=%s", namespace, plan, orNone(nestedString(ip, "status", "phase"))))
			if c := formatConditions(ip, "InstallPlan", namespace, plan, p); c != "" && cause == "" {
				cause = c
			}
		}
	} else {
		p.line(fmt.Sprintf("  Subscription/%s/%s has no InstallPlan yet (OLM has not resolved the operator from its CatalogSource)", namespace, subscription))
	}
	if installed != "" {
		if csv, csvErr := getNamedResource(ctx, runner, kubeconfig, "clusterserviceversion.operators.coreos.com", namespace, installed); csvErr == nil {
			phase := nestedString(csv, "status", "phase")
			reason := nestedString(csv, "status", "reason")
			p.line(fmt.Sprintf("  CSV/%s/%s phase=%s reason=%s", namespace, installed, orNone(phase), orNone(reason)))
			if msg := firstLine(nestedString(csv, "status", "message")); msg != "" {
				p.line("    " + msg)
			}
			if cause == "" && phase != "Succeeded" {
				cause = strings.TrimSpace(fmt.Sprintf("CSV %s %s %s", installed, reason, firstLine(nestedString(csv, "status", "message"))))
			}
		}
	}
	if c := diagnosePullFailures(ctx, runner, kubeconfig, namespace, p); c != "" {
		cause = c
	}
	return clip(strings.TrimSpace(cause), 400)
}

func diagnoseCatalogGate(ctx context.Context, runner OCRunner, kubeconfig, namespace, name string, p *waitProgress) string {
	p.line(fmt.Sprintf("diagnosing why CatalogSource/%s/%s is not READY:", namespace, name))
	var cause string
	obj, err := getNamedResource(ctx, runner, kubeconfig, "catalogsource.operators.coreos.com", namespace, name)
	if err != nil {
		p.line(fmt.Sprintf("  CatalogSource/%s/%s could not be read: %v", namespace, name, err))
	} else {
		state := nestedString(obj, "status", "connectionState", "lastObservedState")
		p.line(fmt.Sprintf("  CatalogSource/%s/%s connectionState=%s", namespace, name, orNone(state)))
		if msg := firstLine(nestedString(obj, "status", "connectionState", "lastConnectError")); msg != "" {
			p.line("    " + msg)
			cause = "CatalogSource " + name + ": " + msg
		}
	}
	if c := diagnosePullFailures(ctx, runner, kubeconfig, namespace, p); c != "" {
		cause = c
	}
	return clip(strings.TrimSpace(cause), 400)
}

func diagnosePullFailures(ctx context.Context, runner OCRunner, kubeconfig, namespace string, p *waitProgress) string {
	pods, err := getResourceList(ctx, runner, kubeconfig, "pods", namespace)
	if err != nil {
		return ""
	}
	var cause string
	shown := 0
	for _, pod := range pods {
		name := nestedString(pod, "metadata", "name")
		for _, cs := range containerStatuses(pod) {
			reason := nestedString(cs, "state", "waiting", "reason")
			if !isPullFailure(reason) {
				continue
			}
			image := nestedString(cs, "image")
			message := firstLine(nestedString(cs, "state", "waiting", "message"))
			if shown < 5 {
				p.line(fmt.Sprintf("  pod %s/%s: %s pulling %s", namespace, name, reason, image))
				if message != "" {
					p.line("    " + message)
				}
				shown++
			}
			if cause == "" {
				cause = strings.TrimSpace(fmt.Sprintf("image pull failed for %s (%s: %s)", image, reason, message))
			}
		}
	}
	return cause
}

const staleConditionLag = 5 * time.Minute

type conditionFinding struct {
	summary string
	problem bool
	stale   bool
}

func formatConditions(obj map[string]any, kind, namespace, name string, p *waitProgress) string {
	return bestFinding(conditionFindings(obj, kind, namespace, name, p))
}

func conditionFindings(obj map[string]any, kind, namespace, name string, p *waitProgress) []conditionFinding {
	conditions, _ := nestedValue(obj, "status", "conditions").([]any)
	newest := newestHeartbeat(conditions)
	var findings []conditionFinding
	for _, item := range conditions {
		cond, ok := item.(map[string]any)
		if !ok {
			continue
		}
		ctype := stringValue(cond["type"])
		cstatus := stringValue(cond["status"])
		creason := stringValue(cond["reason"])
		if !conditionNoteworthy(ctype, cstatus, creason) {
			continue
		}
		cmsg := firstLine(stringValue(cond["message"]))
		line := fmt.Sprintf("  %s/%s/%s %s=%s reason=%s", kind, namespace, name, ctype, cstatus, orNone(creason))
		lag, stale := conditionLag(cond, newest)
		if stale {
			line += fmt.Sprintf(" (stale: last heartbeat %s behind this object's newest condition)", lag)
		}
		p.line(line)
		if cmsg != "" {
			p.line("    " + cmsg)
		}
		findings = append(findings, conditionFinding{
			summary: strings.TrimSpace(fmt.Sprintf("%s %s: %s", ctype, creason, cmsg)),
			problem: problemVocabulary(creason) || problemVocabulary(cmsg),
			stale:   stale,
		})
	}
	return findings
}

func bestFinding(groups ...[]conditionFinding) string {
	for _, want := range []conditionFinding{
		{problem: true, stale: false},
		{problem: false, stale: false},
		{problem: true, stale: true},
		{problem: false, stale: true},
	} {
		for _, group := range groups {
			for _, finding := range group {
				if finding.problem == want.problem && finding.stale == want.stale && finding.summary != "" {
					return finding.summary
				}
			}
		}
	}
	return ""
}

func newestHeartbeat(conditions []any) time.Time {
	var newest time.Time
	for _, item := range conditions {
		cond, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if at, ok := conditionHeartbeat(cond); ok && at.After(newest) {
			newest = at
		}
	}
	return newest
}

func conditionHeartbeat(cond map[string]any) (time.Time, bool) {
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(stringValue(cond["lastHeartbeatTime"])))
	if err != nil {
		return time.Time{}, false
	}
	return at, true
}

func conditionLag(cond map[string]any, newest time.Time) (time.Duration, bool) {
	at, ok := conditionHeartbeat(cond)
	if !ok || newest.IsZero() {
		return 0, false
	}
	lag := newest.Sub(at)
	if lag < staleConditionLag {
		return 0, false
	}
	return lag.Truncate(time.Second), true
}

func conditionNoteworthy(ctype, cstatus, creason string) bool {
	if creason == "" {
		return false
	}
	if problemVocabulary(ctype) || transitionVocabulary(ctype) {
		return cstatus != "False"
	}
	return cstatus != "True"
}

func transitionVocabulary(value string) bool {
	return strings.Contains(strings.ToLower(value), "progress")
}

func problemVocabulary(value string) bool {
	lower := strings.ToLower(value)
	for _, token := range []string{"fail", "unhealthy", "degraded", "mismatch", "error"} {
		if strings.Contains(lower, token) {
			return true
		}
	}
	return false
}

func isPullFailure(reason string) bool {
	switch reason {
	case "ImagePullBackOff", "ErrImagePull", "ImageInspectError", "RegistryUnavailable", "InvalidImageName":
		return true
	default:
		return false
	}
}

func containerStatuses(pod map[string]any) []map[string]any {
	var out []map[string]any
	for _, key := range []string{"initContainerStatuses", "containerStatuses"} {
		list, _ := nestedValue(pod, "status", key).([]any)
		for _, item := range list {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

func getResourceList(ctx context.Context, runner OCRunner, kubeconfig, resource, namespace string) ([]map[string]any, error) {
	args := []string{"get", resource, "-o", "json"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	}
	out, err := runner.Run(ctx, kubeconfig, args, nil)
	if err != nil {
		return nil, err
	}
	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		return nil, err
	}
	items, _ := decoded["items"].([]any)
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result, nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
	}
	return s
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n]) + "..."
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "<none>"
	}
	return s
}
