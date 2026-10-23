package clusterobjects

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/canonicaljson"
)

var dnsName = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$`)
var kindName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)
var dataKey = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
var phasePath = regexp.MustCompile(`^status\.[A-Za-z][A-Za-z0-9]*(\.[A-Za-z][A-Za-z0-9]*)*$`)

func validName(value string) bool {
	if len(value) == 0 || len(value) > 253 || !dnsName.MatchString(value) {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
	}
	return true
}

func validateRef(ref Ref) error {
	parts := strings.Split(ref.APIVersion, "/")
	if len(parts) > 2 || !validName(parts[len(parts)-1]) || (len(parts) == 2 && !validName(parts[0])) || !kindName.MatchString(ref.Kind) || !validName(ref.Name) || (ref.Namespaced && !validName(ref.Namespace)) || (!ref.Namespaced && ref.Namespace != "") {
		return fmt.Errorf("invalid cluster object identity %s/%s/%s", ref.Kind, ref.Namespace, ref.Name)
	}
	return nil
}

func identity(ref Ref) string {
	group := ""
	if parts := strings.SplitN(ref.APIVersion, "/", 2); len(parts) == 2 {
		group = parts[0]
	}
	return strings.Join([]string{group, ref.Kind, ref.Namespace, ref.Name}, "/")
}

func Validate(program Program, options Options) error {
	if program.Version != Version {
		return fmt.Errorf("unsupported cluster object program version")
	}
	if len(program.Steps) > MaxSteps {
		return fmt.Errorf("cluster object program exceeds 96 steps")
	}
	objects, checks := 0, len(program.Checks)
	owned := map[string]bool{}
	namespaces := slices.Clone(options.PlacementNamespaces)
	if options.Namespace != "" {
		namespaces = append(namespaces, options.Namespace)
	}
	for _, step := range program.Steps {
		if step.Create != nil && step.Create.Kind == "Namespace" && step.Create.APIVersion == "v1" {
			namespaces = append(namespaces, step.Create.Name)
		}
	}
	for i, step := range program.Steps {
		count := 0
		for _, present := range []bool{step.Create != nil, step.Place != nil, step.CatalogReady != nil, step.Subscribe != nil, step.ApproveDependent != nil, step.Wait != nil} {
			if present {
				count++
			}
		}
		if count != 1 {
			return fmt.Errorf("step %d must have exactly one arm", i)
		}
		ref, err := validateStep(step, options, namespaces, owned)
		checks += len(step.Wait)
		if err != nil {
			return fmt.Errorf("step %d: %w", i, err)
		}
		if ref != nil {
			objects++
			key := identity(*ref)
			if owned[key] {
				return fmt.Errorf("duplicate owned object %s", key)
			}
			owned[key] = true
		}
	}
	if objects > MaxObjects {
		return fmt.Errorf("cluster object program exceeds 64 owned objects")
	}
	if checks > MaxChecks {
		return fmt.Errorf("cluster object program exceeds 32 checks")
	}
	for _, check := range program.Checks {
		if err := validateCheck(check); err != nil {
			return err
		}
	}
	return nil
}

func validateStep(step Step, options Options, namespaces []string, owned map[string]bool) (*Ref, error) {
	var ref *Ref
	var err error
	switch {
	case step.Kind == "create" && step.Create != nil:
		err = validateObject(*step.Create)
		ref = &step.Create.Ref
	case step.Kind == "place" && step.Place != nil:
		err = validatePlacement(*step.Place, namespaces)
		target := Ref{APIVersion: "v1", Kind: "Secret", Namespace: step.Place.Namespace, Name: step.Place.Name, Namespaced: true}
		ref = &target
	case step.Kind == "catalogReady" && step.CatalogReady != nil:
		err = validateRef(*step.CatalogReady)
		if step.CatalogReady.Kind != "CatalogSource" || step.CatalogReady.APIVersion != "operators.coreos.com/v1alpha1" || !owned[identity(*step.CatalogReady)] {
			err = fmt.Errorf("catalogReady requires a previously owned CatalogSource")
		}
	case step.Kind == "subscribe" && step.Subscribe != nil:
		subscription := step.Subscribe
		err = validateObject(subscription.Object)
		ref = &subscription.Object.Ref
		if subscription.Approval != Exact && subscription.Approval != OperatorManaged {
			err = fmt.Errorf("Subscription %s approval %q is unavailable", ref.Name, subscription.Approval)
		}
		if ref.Kind != "Subscription" || ref.APIVersion != "operators.coreos.com/v1alpha1" || !ref.Namespaced || !validName(subscription.CSV) {
			err = fmt.Errorf("invalid subscribe step")
		}
		var body map[string]json.RawMessage
		if canonicaljson.DecodeClosed(subscription.Object.Body, &body) != nil {
			err = fmt.Errorf("invalid Subscription body")
		} else {
			var spec map[string]json.RawMessage
			if canonicaljson.DecodeClosed(body["spec"], &spec) != nil || rawString(spec["installPlanApproval"]) != "Manual" || rawString(spec["startingCSV"]) != subscription.CSV {
				err = fmt.Errorf("Subscription must declare Manual approval and expected startingCSV")
			}
		}
	case step.Kind == "approveDependent" && step.ApproveDependent != nil:
		dependent := step.ApproveDependent
		if !validName(dependent.Namespace) || !validName(dependent.Subscription) || !validName(dependent.Package) || !validName(dependent.Channel) || !validName(dependent.Source) || !validName(dependent.SourceNamespace) || !validName(dependent.CSV) || dependent.Namespace == options.Namespace || !owned[identity(dependent.Creator)] {
			err = fmt.Errorf("invalid dependent Subscription %s/%s", dependent.Namespace, dependent.Subscription)
		}
		if err == nil {
			err = validateRef(dependent.Creator)
		}
	case step.Kind == "wait" && step.Wait != nil:
		if len(step.Wait) == 0 {
			err = fmt.Errorf("empty wait step")
		}
		for _, check := range step.Wait {
			if checkErr := validateCheck(check); checkErr != nil {
				err = checkErr
				break
			}
		}
	default:
		err = fmt.Errorf("unknown or mismatched cluster object step %q", step.Kind)
	}
	return ref, err
}

func rawString(data json.RawMessage) string {
	var value string
	_ = canonicaljson.DecodeClosed(data, &value)
	return value
}

func validateObject(object ObjectEntry) error {
	if err := validateRef(object.Ref); err != nil {
		return err
	}
	if object.Kind == "Secret" {
		return fmt.Errorf("Secret %s requires a placement", object.Name)
	}
	if err := canonicaljson.ProveObject(object.Body); err != nil {
		return fmt.Errorf("object %s body is not canonical", object.Name)
	}
	if object.DesiredDigest != DesiredDigest(object.Body) {
		return fmt.Errorf("object %s desired digest differs", object.Name)
	}
	var body map[string]json.RawMessage
	if canonicaljson.DecodeClosed(object.Body, &body) != nil {
		return fmt.Errorf("invalid object body")
	}
	var metadata map[string]json.RawMessage
	if canonicaljson.DecodeClosed(body["metadata"], &metadata) != nil || rawString(body["apiVersion"]) != object.APIVersion || rawString(body["kind"]) != object.Kind || rawString(metadata["name"]) != object.Name || rawString(metadata["namespace"]) != object.Namespace {
		return fmt.Errorf("object %s body identity differs", object.Name)
	}
	return safeBody(object.Body)
}

func safeBody(data json.RawMessage) error {
	var text string
	if canonicaljson.DecodeClosed(data, &text) == nil {
		if strings.Contains(text, "{{") || strings.Contains(text, "}}") {
			return fmt.Errorf("template delimiter in object body")
		}
		return nil
	}
	var mapping map[string]json.RawMessage
	if len(data) > 0 && data[0] == '{' && canonicaljson.DecodeClosed(data, &mapping) == nil {
		for key, value := range mapping {
			if key == "status" || key == "managedFields" || key == OwnerAnnotation || strings.Contains(key, "{{") || strings.Contains(key, "}}") {
				return fmt.Errorf("reserved field in object body: %s", key)
			}
			if err := safeBody(value); err != nil {
				return err
			}
		}
	}
	var sequence []json.RawMessage
	if len(data) > 0 && data[0] == '[' && canonicaljson.DecodeClosed(data, &sequence) == nil {
		for _, value := range sequence {
			if err := safeBody(value); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateCheck(check Check) error {
	count := 0
	for _, present := range []bool{check.CSVSucceeded != nil, check.Condition != nil, check.ResourceExists != nil, check.StatusPhase != nil} {
		if present {
			count++
		}
	}
	if count != 1 {
		return fmt.Errorf("check must have exactly one arm")
	}
	switch {
	case check.CSVSucceeded != nil:
		csv := check.CSVSucceeded
		if !validName(csv.Namespace) || !validName(csv.Subscription) || (csv.CSV != "" && !validName(csv.CSV)) {
			return fmt.Errorf("invalid CSV check")
		}
	case check.Condition != nil:
		condition := check.Condition
		if err := validateRef(condition.Ref); err != nil {
			return err
		}
		if condition.Type == "" || !conditionStatus(condition.Status) {
			return fmt.Errorf("invalid condition check")
		}
		for _, failure := range condition.FailOn {
			if failure.Type == "" || !conditionStatus(failure.Status) {
				return fmt.Errorf("invalid failure condition")
			}
		}
	case check.ResourceExists != nil:
		return validateRef(*check.ResourceExists)
	case check.StatusPhase != nil:
		phase := check.StatusPhase
		if err := validateRef(phase.Ref); err != nil {
			return err
		}
		if (phase.Path != "" && !phasePath.MatchString(phase.Path)) || len(phase.Values) == 0 {
			return fmt.Errorf("invalid status phase check")
		}
		for _, value := range phase.Values {
			if value == "" {
				return fmt.Errorf("empty ready phase")
			}
		}
	}
	return nil
}

func conditionStatus(status string) bool {
	return status == "True" || status == "False" || status == "Unknown"
}

func AllowedProduced(ref ProducedRef) bool {
	return ref.Kind == "StorageExport" && ref.Output == "external-details" && validName(ref.Object)
}

func validatePlacement(entry PlacementEntry, namespaces []string) error {
	p := entry.SecretPlacement
	fail := func(reason string) error {
		return fmt.Errorf("Secret placement %s/%s: %s", p.Namespace, p.Name, reason)
	}
	if p.Produced != nil {
		if !AllowedProduced(*p.Produced) || p.Secret != "" || len(p.Keys) != 0 || len(p.Strings) != 0 || p.Type != "Opaque" || !dataKey.MatchString(p.ProducedKey) || len(p.ProducedKey) > 253 {
			return fail("produced source is outside the closed allowlist")
		}
	}
	if p.Produced == nil && (!validName(p.Secret) || p.ProducedKey != "") || !validName(p.Name) || !validName(p.Namespace) || !slices.Contains(namespaces, p.Namespace) {
		return fail("invalid source or namespace")
	}
	if p.Type != "Opaque" && p.Type != "kubernetes.io/tls" {
		return fail("unsupported type")
	}
	if len(p.Keys) == 0 && p.Produced == nil {
		return fail("no mapped parts")
	}
	keys, parts := map[string]bool{}, map[string]bool{}
	for _, key := range p.Keys {
		if !dataKey.MatchString(key.Key) || len(key.Key) > 253 || key.Part == "" || keys[key.Key] || parts[key.Part] {
			return fail("invalid or repeated key or part")
		}
		keys[key.Key], parts[key.Part] = true, true
	}
	for key := range p.Strings {
		if !dataKey.MatchString(key) || len(key) > 253 || keys[key] {
			return fail("invalid or repeated non-secret key")
		}
	}
	if p.Type == "kubernetes.io/tls" && (!keys["tls.crt"] || !keys["tls.key"]) {
		return fail("TLS placement requires certificate and private key")
	}
	body, err := Canonical(p)
	if err != nil || entry.DesiredDigest != DesiredDigest(body) {
		return fail("desired digest differs")
	}
	if err := safeBody(body); err != nil {
		return fail("reserved field or template delimiter")
	}
	return nil
}
