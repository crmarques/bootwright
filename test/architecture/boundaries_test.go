package architecture_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

type packageRole string

const (
	domainRole      packageRole = "domain"
	applicationRole packageRole = "application"
	adapterRole     packageRole = "adapter"
	cliRole         packageRole = "CLI"
	technicalRole   packageRole = "technical"
	compositionRole packageRole = "composition"
	embeddedRole    packageRole = "embedded assets"
)

func packageRoles() map[string]packageRole {
	roles := map[string]packageRole{
		"ansible":                                embeddedRole,
		"api/v1alpha1":                           domainRole,
		"internal/substrate":                     domainRole,
		"internal/reconciliation":                domainRole,
		"internal/infrastructureservices":        domainRole,
		"internal/diagnostics":                   technicalRole,
		"internal/desiredstate/customplaybooks":  domainRole,
		"internal/desiredstate/inputfs":          adapterRole,
		"internal/desiredstate/yamlstream":       adapterRole,
		"internal/desiredstate/encoding":         adapterRole,
		"internal/workspace/contextfs":           adapterRole,
		"internal/workspace/selectionfs":         adapterRole,
		"internal/controller/privilege":          adapterRole,
		"internal/controller/hostlinux":          adapterRole,
		"internal/controller/bundlelocal":        adapterRole,
		"internal/controller/ansiblelocal":       adapterRole,
		"internal/controller/nativelocal":        adapterRole,
		"internal/reconciliation/ansiblerunner":  adapterRole,
		"internal/managedos/medialocal":          adapterRole,
		"internal/machine/sshlocal":              adapterRole,
		"internal/secrets/secretstore":           applicationRole,
		"internal/secrets/localkeyring":          adapterRole,
		"internal/secrets/material":              adapterRole,
		"internal/reconciliation/contextguard":   applicationRole,
		"internal/reconciliation/operationstore": applicationRole,
		"cmd/bootwright":                         compositionRole,
		"internal/cli":                           cliRole,
		"internal/availability":                  technicalRole,
		"internal/addons":                        domainRole,
		"internal/containercluster":              domainRole,
		"internal/controller":                    domainRole,
		"internal/desiredstate":                  domainRole,
		"internal/environment":                   domainRole,
		"internal/machine":                       domainRole,
		"internal/managedos":                     domainRole,
		"internal/secrets":                       domainRole,
		"internal/storage":                       domainRole,
		"internal/trust":                         domainRole,
	}
	for _, capability := range []string{
		"addons/catalog", "addons/preflight",
		"infrastructureservices/artifactserver", "infrastructureservices/managedservice",
		"infrastructureservices/dnsserver", "infrastructureservices/ntpserver", "infrastructureservices/proxy",
		"containercluster/access", "containercluster/agentinstall",
		"containercluster/installation", "containercluster/preflight",
		"controller/clients", "controller/prerequisites", "desiredstate/compilation",
		"environment/access", "environment/inspection", "environment/preflight",
		"machine/access", "machine/inventory", "machine/power", "managedos/media", "managedos/installation",
		"nativeartifacts/rendering", "reconciliation/lifecycle",
		"secrets/custody", "secrets/encryption", "storage/preflight", "storage/rendering",
		"substrate/baremetal", "substrate/libvirt", "trust/enrollment", "workspace/contexts",
	} {
		roles["internal/"+capability] = applicationRole
	}
	return roles
}

// applicationDependencies is the complete set of application packages one
// application package may consume, each because its consumer calls that
// context's published capability rather than reproducing its rules. A new
// entry is a new cross-context coupling and needs its reason stated here.
func applicationDependencies() map[string][]string {
	return map[string][]string{
		// The engine coordinates the contexts an operation crosses.
		"internal/reconciliation/lifecycle": {
			"internal/controller/prerequisites", "internal/desiredstate/compilation",
			"internal/reconciliation/operationstore", "internal/secrets/custody",
			"internal/secrets/secretstore",
		},
		// A lifecycle capability consumes the engine's own port vocabulary and
		// the controller evidence its host was prepared with, and most also the
		// compiler that produced their input and the operation store's log
		// record.
		"internal/controller/clients":                    {"internal/controller/prerequisites", "internal/reconciliation/lifecycle"},
		"internal/substrate/libvirt":                     capabilityDependencies(),
		"internal/substrate/baremetal":                   {"internal/controller/prerequisites", "internal/reconciliation/lifecycle", "internal/reconciliation/operationstore"},
		"internal/managedos/installation":                append(capabilityDependencies(), "internal/infrastructureservices/artifactserver", "internal/infrastructureservices/managedservice"),
		"internal/containercluster/agentinstall":         append(capabilityDependencies(), "internal/infrastructureservices/artifactserver", "internal/infrastructureservices/managedservice"),
		"internal/infrastructureservices/artifactserver": append(capabilityDependencies(), "internal/infrastructureservices/managedservice"),
		"internal/infrastructureservices/managedservice": {"internal/controller/prerequisites", "internal/desiredstate/compilation", "internal/reconciliation/lifecycle"},
		"internal/machine/access":                        {"internal/desiredstate/compilation", "internal/reconciliation/lifecycle"},
		"internal/machine/power":                         {"internal/desiredstate/compilation", "internal/reconciliation/lifecycle"},
		"internal/machine/inventory":                     {"internal/desiredstate/compilation"},
		"internal/trust/enrollment":                      {"internal/desiredstate/compilation"},
		"internal/managedos/media":                       {},
		"internal/controller/prerequisites":              {"internal/desiredstate/compilation"},
		"internal/secrets/custody":                       {"internal/desiredstate/compilation", "internal/secrets/secretstore"},
		"internal/secrets/encryption":                    {"internal/secrets/secretstore"},
		"internal/secrets/secretstore":                   {},
		"internal/workspace/contexts":                    {"internal/desiredstate/compilation", "internal/secrets/secretstore"},
		"internal/desiredstate/compilation":              {},
		// The guard implements a Workspace-owned interface, so it consumes that
		// package's vocabulary and nothing else.
		"internal/reconciliation/contextguard":   {"internal/workspace/contexts"},
		"internal/reconciliation/operationstore": {},
		// Each named service is one managed-service definition and consumes only
		// the package whose capability runs it.
		"internal/infrastructureservices/dnsserver": {"internal/infrastructureservices/managedservice"},
		"internal/infrastructureservices/ntpserver": {"internal/infrastructureservices/managedservice"},
		"internal/infrastructureservices/proxy":     {"internal/infrastructureservices/managedservice"},
	}
}

func capabilityDependencies() []string {
	return []string{
		"internal/controller/prerequisites", "internal/desiredstate/compilation",
		"internal/reconciliation/lifecycle", "internal/reconciliation/operationstore",
	}
}

func permitsDependency(consumer, provider packageRole) bool {
	switch consumer {
	case domainRole:
		return provider == domainRole || provider == technicalRole
	case applicationRole:
		return provider == domainRole || provider == applicationRole || provider == technicalRole
	case cliRole:
		return provider != adapterRole && provider != compositionRole && provider != embeddedRole
	case embeddedRole:
		return provider == technicalRole
	case technicalRole:
		return provider == technicalRole
	case adapterRole:
		return provider != cliRole && provider != compositionRole && provider != adapterRole
	case compositionRole:
		return true
	default:
		return false
	}
}

type sourceFile struct {
	path    string
	owner   string
	syntax  *ast.File
	imports []packageImport
}

type packageImport struct {
	alias string
	path  string
}

func productionSources(t *testing.T) []sourceFile {
	t.Helper()
	var sources []sourceFile
	root := filepath.Join("..", "..")
	for _, directory := range []string{"api", "internal", "ansible", filepath.Join("cmd", "bootwright")} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			syntax, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			source := sourceFile{path: filepath.ToSlash(relative), owner: filepath.ToSlash(filepath.Dir(relative)), syntax: syntax}
			for _, imp := range syntax.Imports {
				name, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					return err
				}
				alias := filepath.Base(name)
				if imp.Name != nil {
					alias = imp.Name.Name
				}
				source.imports = append(source.imports, packageImport{alias: alias, path: name})
			}
			sources = append(sources, source)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return sources
}

func TestPackageDependencyDirection(t *testing.T) {
	roles := packageRoles()
	owners, consumed := map[string]bool{}, map[string]bool{}
	for _, source := range productionSources(t) {
		owners[source.owner] = true
		consumer, ok := roles[source.owner]
		if !ok {
			t.Errorf("%s has no declared package role", source.owner)
			continue
		}
		for _, imported := range source.imports {
			providerPath, local := strings.CutPrefix(imported.path, "github.com/crmarques/bootwright/")
			if !local {
				continue
			}
			provider, ok := roles[providerPath]
			if !ok {
				t.Errorf("%s imports unclassified package %s", source.path, providerPath)
				continue
			}
			if !permitsDependency(consumer, provider) {
				t.Errorf("%s (%s) depends on %s (%s)", source.path, consumer, providerPath, provider)
				continue
			}
			if consumer == applicationRole && provider == applicationRole && source.owner != providerPath {
				allowed, declared := applicationDependencies()[source.owner]
				if !declared {
					t.Errorf("%s consumes another application package but declares no dependency set", source.owner)
					continue
				}
				if !slices.Contains(allowed, providerPath) {
					t.Errorf("%s consumes %s without declaring why in applicationDependencies", source.path, providerPath)
				}
				consumed[source.owner+" "+providerPath] = true
			}
		}
	}
	for _, owner := range sortedKeys(roles) {
		if !owners[owner] {
			t.Errorf("packageRoles classifies %s, which has no production source; remove it", owner)
		}
	}
	for _, consumer := range sortedKeys(applicationDependencies()) {
		if roles[consumer] != applicationRole || !owners[consumer] {
			t.Errorf("applicationDependencies declares %s, which is not an application package with production source; remove it", consumer)
			continue
		}
		for _, provider := range applicationDependencies()[consumer] {
			if !consumed[consumer+" "+provider] {
				t.Errorf("applicationDependencies lets %s consume %s, which it no longer imports; remove the edge", consumer, provider)
			}
		}
	}
}

func TestAdmissionEffectBoundary(t *testing.T) {
	grants := effectGrants()
	owners, needed := map[string]bool{}, map[string]bool{}
	for _, source := range productionSources(t) {
		owners[source.owner] = true
		if source.owner == "cmd/bootwright" {
			continue
		}
		held := append(slices.Clone(grants[everyPackage]), grants[source.owner]...)
		violations := effectViolations(source, held)
		for _, violation := range violations {
			t.Error(violation)
		}
		for index, capability := range held {
			revoked := slices.Delete(slices.Clone(held), index, index+1)
			for _, violation := range effectViolations(source, revoked) {
				if !slices.Contains(violations, violation) {
					owner := source.owner
					if index < len(grants[everyPackage]) {
						owner = everyPackage
					}
					needed[owner+" "+capability] = true
				}
			}
		}
	}
	if !owners[readOnlyInput] {
		t.Errorf("the effect boundary restricts %s, which has no production source; remove the restriction", readOnlyInput)
	}
	for _, owner := range sortedKeys(grants) {
		if owner != everyPackage && !owners[owner] {
			t.Errorf("the effect boundary grants %s exceptions, but it has no production source; remove its grants", owner)
			continue
		}
		for _, capability := range grants[owner] {
			if !needed[owner+" "+capability] {
				holder := owner
				if owner == everyPackage {
					holder = "every package"
				}
				t.Errorf("the effect boundary grants %s %s, which it no longer needs; remove the grant", holder, capability)
			}
		}
	}
}

// everyPackage holds the exceptions every production package has: parsing an
// address or a MAC address reaches no network.
const everyPackage = ""

// readOnlyInput holds syscall only to read the operator's input: it never
// opens a file for writing or mutates one.
const readOnlyInput = "internal/desiredstate/inputfs"

// effectGrants is every exception to the effect boundary, keyed by the package
// that holds it. A capability is an import path, which grants the whole
// package, or an import path and one member, such as net.IP, which grants
// that member alone. Each one fails once its package no longer needs it, so a
// package lists exactly the capabilities it uses.
func effectGrants() map[string][]string {
	return map[string][]string{
		everyPackage:   {"net.ParseMAC", "net/netip", "net/url"},
		"internal/cli": {"fmt.Fprintf", "fmt.Fprintln", "github.com/spf13/cobra", "github.com/spf13/pflag", "io"},
		// The input adapter reads through the handles it holds and names no
		// other os member.
		readOnlyInput:                           {"io", "os.DirEntry", "os.File", "os.NewFile", "syscall"},
		"internal/desiredstate/yamlstream":      {"io"},
		"internal/workspace/contextfs":          {"crypto/rand", "io", "os", "syscall", "unsafe"},
		"internal/workspace/selectionfs":        {"crypto/rand", "io", "os", "os/exec", "syscall"},
		"internal/workspace/contexts":           {"io"},
		"internal/secrets/material":             {"crypto/rand", "io", "net.IP", "os", "syscall"},
		"internal/secrets/localkeyring":         {"crypto/rand", "io"},
		"internal/reconciliation/contextguard":  {"io"},
		"internal/reconciliation/ansiblerunner": {"io", "os", "os/exec", "syscall"},
		"internal/controller/privilege":         {"io", "os", "os/exec", "os/signal", "syscall"},
		"internal/controller/hostlinux":         {"golang.org/x/sys/unix", "io", "os"},
		"internal/controller/bundlelocal": {
			"golang.org/x/sys/unix", "io", "net.Conn", "net.DNSError", "net.Dialer", "net.JoinHostPort",
			"net.Listen", "net.Listener", "net/http", "os", "os/exec", "syscall",
		},
		"internal/controller/ansiblelocal": {"io", "os", "os/exec", "syscall"},
		"internal/controller/nativelocal":  {"golang.org/x/sys/unix", "io", "net/http", "os", "os/exec", "syscall"},
		// The SSH session adapter runs the one pinned client as a child and
		// hands it the operator's streams and its material descriptors, and
		// resolves an offered key to a path-only descriptor before trusting it.
		"internal/machine/sshlocal": {"golang.org/x/sys/unix", "io", "os", "os/exec", "syscall"},
		// Machine access names those streams to hand them on; it opens nothing.
		"internal/machine/access": {"io"},
		// Media acquisition is the one adapter that opens an operator-named file
		// or one authorized endpoint; it runs no process and holds no state.
		"internal/managedos/medialocal": {"net.Dialer", "net/http", "os"},
	}
}

// effectViolations reports the effect capabilities one file uses beyond the
// capabilities its package holds.
func effectViolations(source sourceFile, held []string) []string {
	holds := func(capability string) bool { return slices.Contains(held, capability) }
	// A package holding members of an import may import it and name only them.
	holdsMembers := func(path string) bool {
		return slices.ContainsFunc(held, func(capability string) bool { return strings.HasPrefix(capability, path+".") })
	}
	var violations []string
	paths := map[string]string{}
	for _, imported := range source.imports {
		name := imported.path
		paths[imported.alias] = name
		if strings.HasPrefix(name, "math/rand") || effectImport(name) && !holds(name) && !holdsMembers(name) {
			violations = append(violations, fmt.Sprintf("%s imports unauthorized effect capability %s", source.path, name))
		}
		if (strings.Contains(name, "/internal/cli") || strings.HasPrefix(name, "github.com/spf13/") || name == "io") && !holds(name) {
			violations = append(violations, fmt.Sprintf("%s depends on presentation or unrestricted I/O %s", source.path, name))
		}
	}
	ast.Inspect(source.syntax, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		qualifier, ok := selector.X.(*ast.Ident)
		if !ok || qualifier.Obj != nil {
			return true
		}
		name, imported := paths[qualifier.Name]
		if !imported {
			return true
		}
		member := selector.Sel.Name
		if name == "syscall" && source.owner == readOnlyInput && (member == "O_WRONLY" || member == "O_RDWR" || member == "O_CREAT" || member == "O_TRUNC" || member == "Write" || member == "Unlink" || member == "Rename") {
			violations = append(violations, fmt.Sprintf("%s grants input mutation through syscall.%s", source.path, member))
		}
		if holds(name) || holds(name+"."+member) {
			return true
		}
		switch {
		case name == "net":
			violations = append(violations, fmt.Sprintf("%s accesses networking through net.%s", source.path, member))
		case name == "fmt" && (strings.HasPrefix(member, "Print") || strings.HasPrefix(member, "Fprint") || strings.HasPrefix(member, "Scan") || strings.HasPrefix(member, "Fscan")):
			violations = append(violations, fmt.Sprintf("%s performs presentation outside the CLI through fmt.%s", source.path, member))
		case effectImport(name) && holdsMembers(name):
			violations = append(violations, fmt.Sprintf("%s names %s.%s beyond the members its package holds", source.path, name, member))
		}
		return true
	})
	return violations
}

// effectImport reports whether importing name reaches the filesystem, a
// process, the network, the kernel, randomness or unchecked memory.
func effectImport(name string) bool {
	return strings.HasPrefix(name, "os/") || strings.HasPrefix(name, "net/") || strings.HasPrefix(name, "golang.org/x/sys") || name == "os" || name == "syscall" || name == "crypto/rand" || name == "unsafe"
}

func TestSecretImplementationsRemainBehindPorts(t *testing.T) {
	qualified := false
	neutral := map[string]bool{"internal/secrets/secretstore": false, "internal/secrets/custody": false, "internal/secrets/encryption": false}
	for _, source := range productionSources(t) {
		for _, imported := range source.imports {
			if source.owner == "internal/secrets/material" && imported.path == "golang.org/x/crypto/ssh" {
				qualified = true
			} else if strings.HasPrefix(imported.path, "golang.org/x/crypto/") {
				t.Errorf("%s imports an unqualified cryptographic dependency %s", source.path, imported.path)
			}
			concrete := strings.HasSuffix(imported.path, "/internal/secrets/localkeyring") || strings.HasSuffix(imported.path, "/internal/secrets/material")
			if concrete && source.owner != "cmd/bootwright" {
				t.Errorf("%s imports a concrete secret implementation", source.path)
			}
		}
		if _, checked := neutral[source.owner]; checked {
			neutral[source.owner] = true
			ast.Inspect(source.syntax, func(node ast.Node) bool {
				if literal, ok := node.(*ast.BasicLit); ok && literal.Kind == token.STRING {
					value, _ := strconv.Unquote(literal.Value)
					if value == "local-keyring" || value == "local-v1" || value == "local-keyfile-v1" {
						t.Errorf("%s embeds a concrete secret implementation identity", source.path)
					}
				}
				return true
			})
		}
	}
	if !qualified {
		t.Error("internal/secrets/material no longer imports golang.org/x/crypto/ssh; remove its qualification")
	}
	for _, owner := range sortedKeys(neutral) {
		if !neutral[owner] {
			t.Errorf("%s has no production source; remove it from the implementation-neutral packages", owner)
		}
	}
}
