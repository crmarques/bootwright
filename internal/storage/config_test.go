package storage

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func memoryCeph(config api.Value) api.Value {
	return m("distribution", "ibm", "release", "9.9.1.2", "packageVersion", "20.2.1-405.el9cp", "config", config)
}

func memoryOptions(target any) api.Value {
	return m("osd", m("osd_memory_target_autotune", "false", "osd_memory_target", target))
}

func TestCoreCephConfigPreservesExplicitNativeOptionsAndRefusesUnqualifiedEffects(t *testing.T) {
	for _, test := range []struct {
		name, field string
		config      api.Value
	}{
		{"minimum target", "", memoryOptions("2147483648")},
		{"larger target", "", memoryOptions("4294967296")},
		{"native uint64 ceiling", "", memoryOptions("18446744073709551615")},
		{"unset", "", api.MapValue()},
		{"empty native section", "", m("osd", api.MapValue())},
		{"small target", "$.spec.ceph.config.osd.osd_memory_target", memoryOptions("2147483647")},
		{"integer instead of native string", "$.spec.ceph.config.osd.osd_memory_target", memoryOptions(2147483648)},
		{"overflow", "$.spec.ceph.config.osd.osd_memory_target", memoryOptions("18446744073709551616")},
		{"leading zero", "$.spec.ceph.config.osd.osd_memory_target", memoryOptions("02147483648")},
		{"units unqualified", "$.spec.ceph.config.osd.osd_memory_target", memoryOptions("2GiB")},
		{"INI injection", "$.spec.ceph.config.osd.osd_memory_target", memoryOptions("2147483648\n[global]\nauth_client_required=none")},
		{"autotune true", "$.spec.ceph.config.osd.osd_memory_target_autotune", m("osd", m("osd_memory_target_autotune", "true", "osd_memory_target", "2147483648"))},
		{"Boolean instead of native string", "$.spec.ceph.config.osd.osd_memory_target_autotune", m("osd", m("osd_memory_target_autotune", false, "osd_memory_target", "2147483648"))},
		{"unpaired manual target", "$.spec.ceph.config.osd.osd_memory_target_autotune", m("osd", m("osd_memory_target", "2147483648"))},
		{"unpaired autotune option", "$.spec.ceph.config.osd.osd_memory_target", m("osd", m("osd_memory_target_autotune", "false"))},
		{"unqualified option", "$.spec.ceph.config.osd.password", m("osd", m("osd_memory_target_autotune", "false", "osd_memory_target", "2147483648", "password", "never-echo-this"))},
		{"typed field collision", "$.spec.ceph.config.global.public_network", m("global", m("public_network", "192.0.2.0/24"))},
		{"daemon-specific override", "$.spec.ceph.config", m("osd.1", m("osd_memory_target", "2147483648"))},
		{"mask override", "$.spec.ceph.config", m("osd/host:node-1", m("osd_memory_target", "2147483648"))},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := memoryCeph(test.config)
			before := input
			config, issues := CoreCephConfig(input)
			if !input.Equal(before) || strings.Contains(fmt.Sprint(issues), "never-echo-this") {
				t.Fatal("projection mutated or disclosed authored input")
			}
			if test.field != "" {
				requireIssue(t, issues, test.field)
				if config != nil {
					t.Fatal("refused configuration published a projection")
				}
				return
			}
			if len(issues) != 0 {
				t.Fatal(issues)
			}
			if test.config.Get("osd").Len() == 0 {
				if config != nil {
					t.Fatal("unset options materialized defaults")
				}
			} else {
				want := map[string]map[string]string{"osd": {"osd_memory_target_autotune": "false", "osd_memory_target": test.config.Get("osd", "osd_memory_target").Text()}}
				if !reflect.DeepEqual(config, want) {
					t.Fatalf("got %#v want %#v", config, want)
				}
			}
		})
	}
}

func TestCoreCephConfigRequiresItsReleaseRecordAndJoinsAdmission(t *testing.T) {
	for _, path := range []string{"distribution", "release", "packageVersion"} {
		input := memoryCeph(memoryOptions("2147483648")).With(path, api.StringValue("unqualified"))
		_, issues := CoreCephConfig(input)
		requireIssue(t, issues, "$.spec.ceph.config")
	}
	catalog := fixtureCatalog()
	cluster := find(t, catalog, api.StorageCluster, "storage")
	changed := cluster.WithSpec(cluster.Spec().WithPath(memoryOptions("2147483648"), "ceph", "config"))
	requireIssue(t, Validate(changed, replace(catalog, changed)), "$.spec.ceph.config")
	qualified := changed.Spec().Get("ceph").With("distribution", api.StringValue("ibm")).With("release", api.StringValue("9.9.1.2")).With("packageVersion", api.StringValue("20.2.1-405.el9cp"))
	a := admission{}
	a.clusterServices(qualified)
	for _, issue := range a.issues {
		if strings.HasPrefix(issue.Field, "$.spec.ceph.config") {
			t.Fatal("qualified memory leaves did not join cluster admission", issue)
		}
	}
}

func TestCorePoolDefaultsAreExplicitNativeStringsAndNeverMaterialized(t *testing.T) {
	defaults := m("osd_pool_default_size", "2", "osd_pool_default_min_size", "2")
	for _, memory := range []bool{false, true} {
		config := m("global", defaults)
		want := map[string]map[string]string{"global": {"osd_pool_default_size": "2", "osd_pool_default_min_size": "2"}}
		if memory {
			config = config.With("osd", memoryOptions("2147483648").Get("osd"))
			want["osd"] = map[string]string{"osd_memory_target_autotune": "false", "osd_memory_target": "2147483648"}
		}
		got, issues := CoreCephConfig(memoryCeph(config))
		if len(issues) != 0 || !reflect.DeepEqual(got, want) {
			t.Fatalf("native pairs: got %v, issues %v", got, issues)
		}
	}
	for _, test := range []struct {
		name, field string
		global      api.Value
	}{
		{"unpaired", "$.spec.ceph.config.global.osd_pool_default_min_size", m("osd_pool_default_size", "2")},
		{"extra option", "$.spec.ceph.config.global.password", defaults.With("password", api.StringValue("never-echo-this"))},
		{"undecodable option name", "$.spec.ceph.config.global", defaults.With("bad\nname", api.StringValue("2"))},
		{"integer", "$.spec.ceph.config.global.osd_pool_default_size", defaults.With("osd_pool_default_size", api.IntegerValue("2"))},
		{"three replicas", "$.spec.ceph.config.global.osd_pool_default_size", defaults.With("osd_pool_default_size", api.StringValue("3"))},
		{"unsafe minimum", "$.spec.ceph.config.global.osd_pool_default_min_size", defaults.With("osd_pool_default_min_size", api.StringValue("1"))},
		{"noncanonical", "$.spec.ceph.config.global.osd_pool_default_min_size", defaults.With("osd_pool_default_min_size", api.StringValue("02"))},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, issues := CoreCephConfig(memoryCeph(m("global", test.global)))
			requireIssue(t, issues, test.field)
			if got != nil || strings.Contains(fmt.Sprint(issues), "never-echo-this") {
				t.Fatal("refused pool defaults were materialized or disclosed")
			}
		})
	}
	got, issues := CoreCephConfig(memoryCeph(m("global", api.MapValue())))
	if got != nil || len(issues) != 0 {
		t.Fatal("empty native pool defaults materialized a value")
	}
}

func TestCoreCephConfigRefusalsNameTheOptionToRemoveOrAdd(t *testing.T) {
	_, issues := CoreCephConfig(memoryCeph(m("global", m("osd_pool_default_size", "2", "mon_allow_pool_delete", "true", "bad\nname", "never-echo-this"))))
	want := map[string]string{
		"$.spec.ceph.config.global.mon_allow_pool_delete":     "remove mon_allow_pool_delete from $.spec.ceph.config.global; ",
		"$.spec.ceph.config.global":                           "remove the unqualified option from $.spec.ceph.config.global; ",
		"$.spec.ceph.config.global.osd_pool_default_min_size": "declare osd_pool_default_min_size in $.spec.ceph.config.global; ",
	}
	if len(issues) != len(want) || strings.Contains(fmt.Sprint(issues), "never-echo-this") || strings.Contains(fmt.Sprint(issues), "bad\nname") {
		t.Fatalf("config refusals: %#v", issues)
	}
	for _, issue := range issues {
		if prefix, ok := want[issue.Field]; !ok || !strings.HasPrefix(issue.Remediation, prefix) {
			t.Fatalf("config refusal %#v does not name its option", issue)
		}
		delete(want, issue.Field)
	}
	if len(want) != 0 {
		t.Fatalf("config refusals omitted %v: %#v", want, issues)
	}
}
