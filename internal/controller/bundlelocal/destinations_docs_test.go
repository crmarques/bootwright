package bundlelocal

import (
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// destinationSources are the files that name every host setup and the
// controller stage contact: the publishers and repositories they resolve and
// acquire from, the hosts their Go transports admit, and, in the collection,
// the hosts an Ansible-owned download may be redirected to.
var destinationSources = []string{
	"acquisition.go", "bootstrap.go", "bootstrap_linux_amd64.go", "tools.go", "tools_transport.go",
	filepath.Join("..", "nativelocal", "resolver_linux_amd64.go"),
}

const (
	redirectHostsSource = "../../../ansible/collections/ansible_collections/bootwright/core/plugins/module_utils/controller_files.py"
	operatorGuide       = "../../../docs/operator-guide.md"
	destinationsHeading = "#### Destinations to allow"
)

// syntaxVisitor visits every node of a syntax tree.
type syntaxVisitor func(ast.Node)

func (visit syntaxVisitor) Visit(node ast.Node) ast.Visitor {
	if node != nil {
		visit(node)
	}
	return visit
}

// codeDestinations collects the host of every https:// string literal and every
// host a switch on a URL's Hostname admits, from the syntax tree, so a host a
// comment mentions is not one the code contacts.
func codeDestinations(t *testing.T) map[string]string {
	t.Helper()
	hosts := map[string]string{}
	for _, name := range destinationSources {
		files := token.NewFileSet()
		syntax, err := parser.ParseFile(files, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		record := func(literal ast.Expr, host func(string) string) {
			value, ok := literal.(*ast.BasicLit)
			if !ok || value.Kind != token.STRING {
				return
			}
			text, err := strconv.Unquote(value.Value)
			if err != nil {
				t.Fatal(err)
			}
			if found := host(text); found != "" {
				hosts[found] = files.Position(value.Pos()).String()
			}
		}
		ast.Walk(syntaxVisitor(func(node ast.Node) {
			switch typed := node.(type) {
			case *ast.BasicLit:
				record(typed, func(text string) string {
					if !strings.HasPrefix(text, "https://") {
						return ""
					}
					parsed, err := url.Parse(text)
					if err != nil || parsed.Hostname() == "" {
						t.Fatalf("%s: %q is no URL with a host", files.Position(typed.Pos()), text)
					}
					return parsed.Hostname()
				})
			case *ast.SwitchStmt:
				call, ok := typed.Tag.(*ast.CallExpr)
				if !ok {
					return
				}
				if selector, ok := call.Fun.(*ast.SelectorExpr); !ok || selector.Sel.Name != "Hostname" {
					return
				}
				for _, statement := range typed.Body.List {
					for _, expression := range statement.(*ast.CaseClause).List {
						record(expression, func(text string) string { return text })
					}
				}
			}
		}), syntax)
	}
	data, err := os.ReadFile(redirectHostsSource)
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)\nREDIRECT_HOSTS = frozenset\(\n    \(\n(.*?)\n    \)\n\)`).FindSubmatch(data)
	if block == nil {
		t.Fatalf("%s no longer declares REDIRECT_HOSTS as one frozenset of a tuple", redirectHostsSource)
	}
	for _, line := range strings.Split(string(block[1]), "\n") {
		host, err := strconv.Unquote(strings.TrimSuffix(strings.TrimSpace(line), ","))
		if err != nil {
			t.Fatalf("%s: REDIRECT_HOSTS holds %q, which is no quoted host", redirectHostsSource, line)
		}
		hosts[host] = redirectHostsSource
	}
	return hosts
}

// guideDestinations reads the hosts the operator guide's proxied-network
// section lists, one bullet each, under its destinations heading.
func guideDestinations(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(operatorGuide)
	if err != nil {
		t.Fatal(err)
	}
	_, section, found := strings.Cut(string(data), "\n"+destinationsHeading+"\n")
	if !found {
		t.Fatalf("%s has no %q section", operatorGuide, destinationsHeading)
	}
	section, _, _ = strings.Cut(section, "\n#")
	var hosts []string
	for _, line := range strings.Split(section, "\n") {
		if host, found := strings.CutPrefix(line, "- `"); found {
			host, _, _ = strings.Cut(host, "`")
			hosts = append(hosts, host)
		}
	}
	return hosts
}

// An allowlisting proxy admits only the hosts the guide lists, so that list is
// exactly what setup and the controller stage contact: a host the code adds
// and the guide omits is refused at the proxy, and one the guide keeps after
// the code dropped it is a needless opening.
func TestDocsProxiedNetworkDestinationsMatchTheCode(t *testing.T) {
	code := codeDestinations(t)
	listed := guideDestinations(t)
	if !slices.IsSorted(listed) || len(slices.Compact(slices.Clone(listed))) != len(listed) {
		t.Errorf("%s lists its destinations %v out of order or twice", operatorGuide, listed)
	}
	for _, host := range slices.Sorted(maps.Keys(code)) {
		if !slices.Contains(listed, host) {
			t.Errorf("%s contacts %s, which %s does not list under %q", code[host], host, operatorGuide, destinationsHeading)
		}
	}
	for _, host := range listed {
		if _, found := code[host]; !found {
			t.Errorf("%s lists %s, which no destination source contacts", operatorGuide, host)
		}
	}
}
