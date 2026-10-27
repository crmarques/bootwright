package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const substratePackage = "github.com/crmarques/bootwright/internal/substrate"

// implementationIdentityAllowlist names every production file outside an
// owning package, composition and admission that still reaches an arm's or an
// implementation's specifics directly, by "<file>: <use>", with the backlog
// item that removes it. It is exact and only shrinks: a new use fails, and a
// row whose use is gone fails until it is removed (specs/architecture.md,
// Implementation selection).
func implementationIdentityAllowlist() map[string]string {
	return map[string]string{}
}

// coincidentArmWords names every production use of an arm's word that is not
// the arm: a tool's compatibility tag, an add-on's capability, a package
// version key or an installer platform type, by "<file>: <use>", with what
// the word means there. It is exact like the allowlist.
func coincidentArmWords() map[string]string {
	return map[string]string{
		`internal/addons/packages.go: "kubevirt" (substrate.ArmKubeVirt)`:                            "the virtualization add-on package's reader",
		`internal/containercluster/agentinstall/projection.go: "baremetal" (substrate.ArmBaremetal)`: "the installer's platform type",
		`internal/containercluster/openshift/virtual_ips.go: "baremetal" (substrate.ArmBaremetal)`:   "the installer's platform type",
		`internal/controller/bundlelocal/tools.go: "kubevirt" (substrate.ArmKubeVirt)`:               "virtctl's compatibility tag",
		`internal/controller/tools.go: "kubevirt" (substrate.ArmKubeVirt)`:                           "virtctl's compatibility tag and the add-on capability that needs it",
		`internal/controller/versions.go: "libvirt" (substrate.ArmLibvirt)`:                          "the libvirt package's version key",
	}
}

// identityRule is what the rule reads off the production tree: the arm
// values, and each implementation identity constant by its package and name
// with its value.
type identityRule struct {
	arms       map[string]string
	identities map[string]string
	values     map[string]string
}

func implementationIdentityRule(sources []sourceFile) identityRule {
	rule := identityRule{arms: map[string]string{}, identities: map[string]string{}, values: map[string]string{}}
	for _, source := range sources {
		for _, declaration := range source.syntax.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST {
				continue
			}
			for _, spec := range general.Specs {
				value := spec.(*ast.ValueSpec)
				for index, name := range value.Names {
					if index >= len(value.Values) {
						continue
					}
					literal, ok := value.Values[index].(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}
					text, err := strconv.Unquote(literal.Value)
					if err != nil {
						continue
					}
					owner := "github.com/crmarques/bootwright/" + source.owner
					switch {
					case owner == substratePackage && strings.HasPrefix(name.Name, "Arm"):
						rule.arms[text] = "substrate." + name.Name
					case strings.HasSuffix(name.Name, "Implementation"):
						rule.identities[owner+"."+name.Name] = text
						rule.values[text] = owner
					}
				}
			}
		}
	}
	return rule
}

// identityExempt reports whether a file may name arm and implementation
// specifics: the API schema that declares the arms, the substrate packages,
// composition, a domain's admission
// refusals of its own support matrix, and, for one implementation's identity,
// that implementation's own package.
func identityExempt(source sourceFile) bool {
	return strings.HasPrefix(source.owner, "api/") || source.owner == "internal/substrate" || strings.HasPrefix(source.owner, "internal/substrate/") ||
		source.owner == "cmd/bootwright" || strings.Contains(path.Base(source.path), "admission")
}

func implementationIdentityUses(rule identityRule, source sourceFile) []string {
	if identityExempt(source) {
		return nil
	}
	aliases := map[string]string{}
	for _, imported := range source.imports {
		aliases[imported.alias] = imported.path
	}
	owner := "github.com/crmarques/bootwright/" + source.owner
	var uses []string
	ast.Inspect(source.syntax, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.SelectorExpr:
			qualifier, ok := node.X.(*ast.Ident)
			if !ok {
				return true
			}
			imported := aliases[qualifier.Name]
			if imported == substratePackage && strings.HasPrefix(node.Sel.Name, "Arm") {
				uses = append(uses, "substrate."+node.Sel.Name)
			}
			if _, identity := rule.identities[imported+"."+node.Sel.Name]; identity {
				uses = append(uses, path.Base(imported)+"."+node.Sel.Name)
			}
		case *ast.BasicLit:
			if node.Kind != token.STRING {
				return true
			}
			text, err := strconv.Unquote(node.Value)
			if err != nil {
				return true
			}
			if arm, found := rule.arms[text]; found {
				uses = append(uses, strconv.Quote(text)+" ("+arm+")")
			}
			if declaring, found := rule.values[text]; found && declaring != owner {
				uses = append(uses, strconv.Quote(text)+" (an implementation identity)")
			}
		}
		return true
	})
	slices.Sort(uses)
	return slices.Compact(uses)
}

func implementationIdentityViolations(rule identityRule, sources []sourceFile, allowed map[string]string) []string {
	found := map[string]bool{}
	var violations []string
	for _, source := range sources {
		for _, use := range implementationIdentityUses(rule, source) {
			key := source.path + ": " + use
			found[key] = true
			if _, listed := allowed[key]; !listed {
				violations = append(violations, key+" names an arm or implementation outside its owner; reach it through a capability the owner publishes")
			}
		}
	}
	for _, key := range slices.Sorted(maps.Keys(allowed)) {
		if !found[key] {
			violations = append(violations, "implementationIdentityAllowlist excuses "+key+", which no longer occurs; remove it")
		}
	}
	slices.Sort(violations)
	return violations
}

// TestNoProductionCodeBranchesOnAnArmOrImplementationOutsideItsOwner holds
// production code to the selection rule: outside the substrate packages, an
// implementation's own package, composition and admission files, no file
// names a substrate arm constant or value, or another package's
// implementation identity constant or value, unless the shrinking allowlist
// names it with the item that removes it.
func TestNoProductionCodeBranchesOnAnArmOrImplementationOutsideItsOwner(t *testing.T) {
	sources := productionSources(t)
	rule := implementationIdentityRule(sources)
	if len(rule.arms) == 0 || len(rule.identities) == 0 {
		t.Fatalf("the rule reads %d arms and %d implementation identities", len(rule.arms), len(rule.identities))
	}
	allowed := implementationIdentityAllowlist()
	for key, item := range allowed {
		if !regexp.MustCompile(`^B[0-9]+$`).MatchString(item) {
			t.Errorf("implementationIdentityAllowlist excuses %s for %q, which names no backlog item", key, item)
		}
	}
	maps.Copy(allowed, coincidentArmWords())
	for _, violation := range implementationIdentityViolations(rule, sources, allowed) {
		t.Error(violation)
	}
}

// A containercluster file comparing a node's arm with substrate.ArmKubeVirt is
// exactly the use the rule rejects; the same comparison in an admission file
// passes, and an allowlist row naming it excuses it.
func TestTheImplementationIdentityRuleRejectsAnArmComparison(t *testing.T) {
	syntax, err := parser.ParseFile(token.NewFileSet(), "pushed.go", `package agentinstall

import "github.com/crmarques/bootwright/internal/substrate"

func pushed(arm string) bool { return arm == substrate.ArmKubeVirt || arm == "machine-libvirt-v1" }
`, 0)
	if err != nil {
		t.Fatal(err)
	}
	rule := identityRule{
		arms:       map[string]string{"kubevirt": "substrate.ArmKubeVirt"},
		identities: map[string]string{"github.com/crmarques/bootwright/internal/substrate/libvirt.MachineImplementation": "machine-libvirt-v1"},
		values:     map[string]string{"machine-libvirt-v1": "github.com/crmarques/bootwright/internal/substrate/libvirt"},
	}
	fixture := sourceFile{path: "internal/containercluster/agentinstall/pushed.go", owner: "internal/containercluster/agentinstall", syntax: syntax,
		imports: []packageImport{{alias: "substrate", path: substratePackage}}}
	want := []string{
		`internal/containercluster/agentinstall/pushed.go: "machine-libvirt-v1" (an implementation identity) names an arm or implementation outside its owner; reach it through a capability the owner publishes`,
		"internal/containercluster/agentinstall/pushed.go: substrate.ArmKubeVirt names an arm or implementation outside its owner; reach it through a capability the owner publishes",
	}
	if got := implementationIdentityViolations(rule, []sourceFile{fixture}, nil); !slices.Equal(got, want) {
		t.Fatalf("violations = %q, want %q", got, want)
	}
	allowed := map[string]string{"internal/containercluster/agentinstall/pushed.go: substrate.ArmKubeVirt": "B809",
		`internal/containercluster/agentinstall/pushed.go: "machine-libvirt-v1" (an implementation identity)`: "B809",
		"internal/cli/gone.go: substrate.ArmLibvirt":                                                          "B809"}
	if got := implementationIdentityViolations(rule, []sourceFile{fixture}, allowed); !slices.Equal(got, []string{
		"implementationIdentityAllowlist excuses internal/cli/gone.go: substrate.ArmLibvirt, which no longer occurs; remove it",
	}) {
		t.Fatalf("allowlisted violations = %q", got)
	}
	fixture.path = "internal/containercluster/openshift/pushed_admission.go"
	if got := implementationIdentityViolations(rule, []sourceFile{fixture}, nil); len(got) != 0 {
		t.Fatalf("an admission file is refused: %q", got)
	}
}
