package cephcluster

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

var updateSpecTables = flag.Bool("update", false, "regenerate the Ceph specification registry tables")

func TestNativeServiceFamilyTableMatchesAdmission(t *testing.T) {
	var table strings.Builder
	table.WriteString("| Family | Service ID |\n| --- | --- |\n")
	for _, family := range nativeServiceTypes {
		id := ""
		switch family.serviceID {
		case serviceIDForbidden:
			id = "forbidden"
		case serviceIDBootwright:
			id = "Bootwright-owned"
		case serviceIDNative:
			id = "required native ID"
		default:
			t.Fatalf("unknown service ID rule for %s", family.name)
		}
		fmt.Fprintf(&table, "| %s | %s |\n", specCode(family.name), id)
	}
	cephSpecTable(t, "../../../specs/ceph-native-api.md", "| Family | Service ID |", table.String())
}

func TestCephListenerTableMatchesRegistry(t *testing.T) {
	portKeys := map[string]bool{}
	smb := map[string]int64{}
	files := map[string]bool{"endpoints.go": true, "smb.go": true}
	for _, source := range nativeListeners {
		fn := runtime.FuncForPC(reflect.ValueOf(source).Pointer())
		file, _ := fn.FileLine(fn.Entry())
		files[filepath.Base(file)] = true
	}
	for file := range files {
		syntax, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(syntax, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name, ok := call.Fun.(*ast.Ident)
			if !ok || name.Name != "listenerPort" {
				return true
			}
			key, ok := call.Args[1].(*ast.BasicLit)
			if !ok || key.Kind != token.STRING {
				t.Fatal("listenerPort must name a literal member for registry qualification")
			}
			value, err := strconv.Unquote(key.Value)
			if err != nil {
				t.Fatal(err)
			}
			portKeys[value] = true
			return true
		})
		for _, declaration := range syntax.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "smbListeners" {
				continue
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				literal, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				label := ""
				var port int64
				for _, entry := range literal.Elts {
					pair, ok := entry.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := pair.Key.(*ast.Ident)
					if !ok {
						continue
					}
					value, ok := pair.Value.(*ast.BasicLit)
					if !ok {
						continue
					}
					switch key.Name {
					case "label":
						label, err = strconv.Unquote(value.Value)
					case "low":
						port, err = strconv.ParseInt(value.Value, 10, 64)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				if label != "" && port != 0 {
					smb[label] = port
				}
				return true
			})
		}
	}
	if len(portKeys) == 0 || len(smb) == 0 {
		t.Fatal("listener sources were not qualified")
	}
	keys := make([]string, 0, len(portKeys))
	for key := range portKeys {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	ports := map[int64]string{}
	authored := m()
	for i, key := range keys {
		value := int64(20000 + i)
		ports[value] = specCode(key)
		authored = authored.With(key, api.IntegerValue(strconv.FormatInt(value, 10)))
	}
	host := m("name", "node-1", "machineRef", "node-1", "labels", api.StringList("mgr"))
	nfs := b479Service("nfs").With("service_id", api.StringValue("data"))
	cluster := api.NewObject(api.CephCluster, "registry", api.Value{}, m("topology", m("nodes", api.ListValue(host)), "services", api.ListValue(nfs)))
	catalog := api.NewCatalog([]api.Object{cluster})
	var table strings.Builder
	table.WriteString("| Listener source | Default ports | Authored port members and fixed ports |\n| --- | --- | --- |\n")
	for _, row := range reservedListeners {
		spans := []string{}
		for _, span := range row.ranges {
			spans = append(spans, listenerSpan(span[0], span[1]))
		}
		fmt.Fprintf(&table, "| %s | %s | fixed |\n", row.label, strings.Join(spans, ", "))
	}
	families := make([]string, 0, len(nativeListeners))
	for family := range nativeListeners {
		families = append(families, family)
	}
	slices.Sort(families)
	for _, family := range families {
		service := b479Service(family).With("spec", m("backend_service", "nfs.data", "virtual_ip", "192.0.2.8/24"))
		source := nativeListeners[family]
		defaults := listenerPorts(source(cluster, catalog, service, "$.service", 0), nil)
		if family == "rgw" {
			tls := source(cluster, catalog, service.With("spec", service.Get("spec").With("ssl", api.BoolValue(true))), "$.service", 0)
			defaults += "; ssl: " + listenerPorts(tls, nil)
		}
		withPorts := authored.With("backend_service", api.StringValue("nfs.data")).With("virtual_ip", api.StringValue("192.0.2.8/24"))
		configured := source(cluster, catalog, service.With("spec", withPorts), "$.service", 0)
		label := specCode(family)
		if family == "ingress" {
			label += " (NFS backend)"
		}
		fmt.Fprintf(&table, "| %s | %s | %s |\n", label, defaults, listenerPorts(configured, ports))
	}
	labels := make([]string, 0, len(smb))
	for label := range smb {
		labels = append(labels, label)
	}
	slices.Sort(labels)
	for _, label := range labels {
		fmt.Fprintf(&table, "| %s | %d | fixed |\n", label, smb[label])
	}
	cephSpecTable(t, "../../../specs/api/ceph.md", "| Listener source | Default ports | Authored port members and fixed ports |", table.String())
}

func listenerSpan(low, high int64) string {
	if low == high {
		return strconv.FormatInt(low, 10)
	}
	return fmt.Sprintf("%d-%d", low, high)
}

func listenerPorts(listeners []listener, authored map[int64]string) string {
	values := []string{}
	for _, row := range listeners {
		value := authored[row.low]
		if value == "" {
			value = listenerSpan(row.low, row.high)
			if row.label != "" {
				value = strings.TrimSuffix(row.label, " of $.service")
			} else if _, member, ok := strings.Cut(row.field, ".spec."); ok {
				value = specCode(member) + ": " + value
			}
		}
		values = append(values, value)
	}
	slices.Sort(values)
	values = slices.Compact(values)
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

func specCode(value string) string { return string(rune(96)) + value + string(rune(96)) }

func cephSpecTable(t *testing.T, file, header, want string) {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, header)
	if start < 0 {
		t.Fatalf("%s registry is absent from %s", header, file)
	}
	end := start + strings.Index(text[start:], "\n\n")
	want = strings.TrimSuffix(want, "\n")
	if *updateSpecTables {
		if err := os.WriteFile(file, []byte(text[:start]+want+text[end:]), 0o644); err != nil {
			t.Fatal(err)
		}
		data, err = os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text = string(data)
		end = start + strings.Index(text[start:], "\n\n")
	}
	if text[start:end] != want {
		t.Fatalf("%s registry differs from code; run %s with -update", file, t.Name())
	}
}
