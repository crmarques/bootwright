package architecture_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
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
		"internal/secrets/secretstore":           applicationRole,
		"internal/secrets/localkeyring":          adapterRole,
		"internal/secrets/material":              adapterRole,
		"internal/reconciliation/contextguard":   applicationRole,
		"internal/reconciliation/operationstore": applicationRole,
		"cmd/bootwright":                         compositionRole,
		"internal/cli":                           cliRole,
		"internal/availability":                  technicalRole,
	}
	for _, capability := range []string{
		"addons/catalog", "addons/preflight",
		"infrastructureservices/artifactserver", "infrastructureservices/managedservice",
		"infrastructureservices/dnsserver", "infrastructureservices/ntpserver", "infrastructureservices/proxy",
		"containercluster/access", "containercluster/installation", "containercluster/preflight",
		"controller/clients", "controller/prerequisites", "desiredstate/compilation",
		"environment/access", "environment/inspection", "environment/preflight",
		"machine/access", "machine/inventory", "managedos/media",
		"nativeartifacts/rendering", "reconciliation/lifecycle",
		"secrets/custody", "secrets/encryption", "storage/preflight", "storage/rendering",
		"trust/enrollment", "workspace/contexts",
	} {
		owner, _, _ := strings.Cut(capability, "/")
		roles["internal/"+owner] = domainRole
		roles["internal/"+capability] = applicationRole
	}
	return roles
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
	for _, source := range productionSources(t) {
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
			}
		}
	}
}

func TestAdmissionEffectBoundary(t *testing.T) {
	for _, source := range productionSources(t) {
		if source.owner == "cmd/bootwright" {
			continue
		}
		isCLI := source.owner == "internal/cli"
		input := source.owner == "internal/desiredstate/inputfs"
		storage := source.owner == "internal/workspace/contextfs"
		secretMaterial := source.owner == "internal/secrets/material"
		secretStore := source.owner == "internal/secrets/localkeyring"
		guard := source.owner == "internal/reconciliation/contextguard"
		selection := source.owner == "internal/workspace/selectionfs"
		invocation := source.owner == "internal/controller/privilege"
		controllerHost := source.owner == "internal/controller/hostlinux"
		controllerBundle := source.owner == "internal/controller/bundlelocal"
		controllerPackages := source.owner == "internal/controller/ansiblelocal"
		controllerNative := source.owner == "internal/controller/nativelocal"
		controllerEffects := controllerBundle || controllerPackages || controllerNative
		lifecycleRunner := source.owner == "internal/reconciliation/ansiblerunner"
		// Media acquisition is the one adapter that opens an operator-named file
		// or one authorized endpoint; it runs no process and holds no state.
		mediaSource := source.owner == "internal/managedos/medialocal"
		configuration := source.owner == "internal/workspace/contexts"
		localProcess := selection || invocation || controllerEffects || lifecycleRunner
		codec := source.owner == "internal/desiredstate/yamlstream" || source.owner == "internal/desiredstate/encoding"
		for _, imported := range source.imports {
			name := imported.path
			forbidden := (strings.HasPrefix(name, "os/") && !(localProcess && name == "os/exec" || invocation && name == "os/signal")) || strings.HasPrefix(name, "net/") && name != "net/url" && name != "net/netip" && !controllerEffects && !mediaSource || !storage && !selection && !secretMaterial && !secretStore && name == "crypto/rand" || strings.HasPrefix(name, "math/rand") || !storage && !secretMaterial && name == "unsafe" || strings.HasPrefix(name, "golang.org/x/sys") && !controllerHost && !controllerEffects || !input && !storage && !secretMaterial && !localProcess && !controllerHost && !mediaSource && (name == "os" || name == "syscall")
			if forbidden {
				t.Errorf("%s imports unauthorized effect capability %s", source.path, name)
			}
			if !isCLI && (strings.Contains(name, "/internal/cli") || strings.HasPrefix(name, "github.com/spf13/") || name == "io" && !input && !codec && !storage && !guard && !secretMaterial && !secretStore && !configuration && !localProcess && !controllerHost) {
				t.Errorf("%s depends on presentation or unrestricted I/O %s", source.path, name)
			}
			ast.Inspect(source.syntax, func(node ast.Node) bool {
				selector, ok := node.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				qualifier, ok := selector.X.(*ast.Ident)
				if !ok || qualifier.Obj != nil || qualifier.Name != imported.alias {
					return true
				}
				member := selector.Sel.Name
				if name == "net" && member != "ParseMAC" && !controllerEffects && !mediaSource && !(secretMaterial && (member == "IP" || member == "ParseIP")) {
					t.Errorf("%s accesses networking through net.%s", source.path, member)
				}
				if name == "fmt" && !isCLI && (strings.HasPrefix(member, "Print") || strings.HasPrefix(member, "Fprint") || strings.HasPrefix(member, "Scan") || strings.HasPrefix(member, "Fscan")) {
					t.Errorf("%s performs presentation outside the CLI", source.path)
				}
				if name == "os" && input && member != "File" && member != "DirEntry" && member != "NewFile" {
					t.Errorf("%s accesses filesystem outside held handles through os.%s", source.path, member)
				}
				if name == "syscall" && input && (member == "O_WRONLY" || member == "O_RDWR" || member == "O_CREAT" || member == "O_TRUNC" || member == "Write" || member == "Unlink" || member == "Rename") {
					t.Errorf("%s grants input mutation through syscall.%s", source.path, member)
				}
				return true
			})
		}
	}
}

func TestSecretImplementationsRemainBehindPorts(t *testing.T) {
	for _, source := range productionSources(t) {
		for _, imported := range source.imports {
			if strings.HasPrefix(imported.path, "golang.org/x/crypto/") && (source.owner != "internal/secrets/material" || imported.path != "golang.org/x/crypto/ssh") {
				t.Errorf("%s imports an unqualified cryptographic dependency %s", source.path, imported.path)
			}
			concrete := strings.HasSuffix(imported.path, "/internal/secrets/localkeyring") || strings.HasSuffix(imported.path, "/internal/secrets/material")
			if concrete && source.owner != "cmd/bootwright" {
				t.Errorf("%s imports a concrete secret implementation", source.path)
			}
		}
		if source.owner == "internal/secrets/secretstore" || source.owner == "internal/secrets/custody" || source.owner == "internal/secrets/encryption" {
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
}
