package prerequisites

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/controller"
)

// NewResolvedDefinition freezes one context-independent setup resolution,
// bound separately from bundle content: a native transaction's before-state
// changes after success, and that alone must not create another private
// execution bundle on the next run. It carries no target tool: those are
// selected by a context and resolved by its own controller stage.
func NewResolvedDefinition(bootstrap BootstrapDefinition, native NativeResolvedPlan) (Definition, error) {
	if err := ValidateBootstrap(bootstrap); err != nil {
		return Definition{}, err
	}
	if err := ValidateNativePlan(native); err != nil {
		return Definition{}, err
	}
	if bootstrap.Platform != native.Platform || bootstrap.PythonIntent != native.Requests.Python || bootstrap.AnsibleIntent != native.Requests.Ansible {
		return Definition{}, failure("controller.unsupported", "dependency resolutions do not share the same platform and requested versions", "repeat setup with consistent dependency versions")
	}
	for _, action := range native.Actions {
		if slices.Contains(bootstrap.ExecutionPackages, action.After.Name) {
			return Definition{}, failure("controller.unsupported", "native dependencies would replace the qualified execution foundation", "use dependency versions compatible with the provided host foundation")
		}
	}
	value := Definition{Platform: native.Platform, Versions: native.Requests, ToolRequests: []controller.ToolRequest{}, Native: &native, Bootstrap: &bootstrap, NativeRequirements: native.Requirements, PythonVersion: bootstrap.PythonVersion, AnsibleVersion: bootstrap.AnsibleVersion, Execution: bootstrap.Execution, Sources: slices.Clone(bootstrap.Sources)}
	value.Runtime.Files, value.Runtime.Links = []InstalledFile{}, []InstalledLink{}
	for _, item := range native.Packages {
		value.Sources = append(value.Sources, item.Source)
	}
	for _, root := range native.Roots {
		if root.Key == "podman" {
			value.Runtime.Version = root.Package.Version + "-" + root.Package.Release
		}
	}
	// WithTools owns the common source checks. Its base identity is temporary;
	// the complete resolved content receives its own hash.
	value.CatalogDigest = strings.Repeat("0", 64)
	var err error
	value, err = WithTools(value, []ToolDefinition{})
	if err != nil {
		return Definition{}, err
	}
	value.BaseCatalogDigest = ""
	value.CatalogDigest, err = resolvedContentDigest(value)
	if err != nil {
		return Definition{}, err
	}
	value.ResolutionDigest, err = resolvedDefinitionDigest(value)
	if err != nil {
		return Definition{}, err
	}
	return CloneDefinition(value), nil
}

func resolvedContentDigest(value Definition) (string, error) {
	identities := make([]NativeIdentity, 0, len(value.Native.Roots))
	for _, root := range value.Native.Roots {
		identities = append(identities, root.Package)
	}
	return definitionHash("bootwright.controller.resolved-content-v1", struct {
		Platform    Platform
		Bootstrap   string
		Sources     []DependencySource
		Tools       []ToolDefinition
		NativeRoots []NativeIdentity
	}{value.Platform, value.Bootstrap.Digest, value.Sources, value.Tools, identities})
}

func resolvedDefinitionDigest(value Definition) (string, error) {
	value.ResolutionDigest = ""
	return definitionHash("bootwright.controller.resolved-definition-v1", value)
}

func definitionHash(domain string, value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil || len(encoded) > 512<<10 {
		return "", failure("controller.unsupported", "resolved dependencies exceed their canonical evidence bounds", "reduce the selected dependency closure")
	}
	digest := sha256.Sum256(append(append([]byte(domain), 0), encoded...))
	return hex.EncodeToString(digest[:]), nil
}

// ValidateResolvedDefinition checks durable data before any consuming adapter
// grants it authority. Artifact and installed-file verification still belongs
// at the acquisition, execution and native transaction boundaries.
func ValidateResolvedDefinition(value Definition) error {
	if value.Bootstrap == nil || value.Native == nil || len(value.ToolRequests) != 0 || len(value.Tools) != 0 {
		return failure("controller.state", "resolved dependencies lack their complete frozen definition", "restore the exact setup evidence")
	}
	expected, err := NewResolvedDefinition(*value.Bootstrap, *value.Native)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected, value) {
		return failure("controller.state", "resolved dependencies differ from their frozen content identity", "restore the exact setup evidence")
	}
	return nil
}

// CloneDefinition never returns mutable aliases to supplied resolution data.
// Definition contains only JSON values, so marshaling cannot fail.
func CloneDefinition(value Definition) Definition {
	encoded, _ := json.Marshal(value)
	var result Definition
	_ = json.Unmarshal(encoded, &result)
	return result
}

func SameDefinition(a, b Definition) bool {
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	return leftErr == nil && rightErr == nil && bytes.Equal(left, right)
}
