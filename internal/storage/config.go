package storage

import (
	"regexp"
	"slices"
	"strconv"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func CoreCephConfig(ceph api.Value) (map[string]map[string]string, []api.Issue) {
	config := ceph.Get("config")
	if !config.Present() || config.Type() == api.Mapping && config.Len() == 0 {
		return nil, nil
	}
	refuse := func(field, message, remediation string) (map[string]map[string]string, []api.Issue) {
		return nil, []api.Issue{{Code: "api.invariant", Field: field, Message: message, Remediation: remediation}}
	}
	if config.Type() != api.Mapping {
		return refuse("$.spec.ceph.config", "cluster config has an unqualified section or shape", "declare only the documented osd memory or global pool-default pairs")
	}
	result := map[string]map[string]string{}
	for _, section := range config.Fields() {
		if section.Name != "osd" && section.Name != "global" || section.Value.Type() != api.Mapping {
			return refuse("$.spec.ceph.config", "cluster config has an unqualified section or shape", "declare only the documented osd memory or global pool-default pairs")
		}
		if section.Value.Len() == 0 {
			continue
		}
		var values map[string]string
		var issues []api.Issue
		if section.Name == "osd" {
			values, issues = coreOSDMemory(section.Value)
		} else {
			values, issues = corePoolDefaults(section.Value)
		}
		if len(issues) != 0 {
			return nil, issues
		}
		result[section.Name] = values
	}
	if len(result) == 0 {
		return nil, nil
	}
	if ceph.Get("distribution").Text() != "ibm" || ceph.Get("release").Text() != "9.9.1.2" || ceph.Get("packageVersion").Text() != "20.2.1-405.el9cp" {
		return refuse("$.spec.ceph.config", "the declared release has no qualified core configuration record", "use the IBM 9.9.1.2 candidate package coordinates or qualify the selected release before using config")
	}
	return result, nil
}

func coreOSDMemory(osd api.Value) (map[string]string, []api.Issue) {
	refuse := func(field, message, remediation string) (map[string]string, []api.Issue) {
		return nil, []api.Issue{{Code: "api.invariant", Field: field, Message: message, Remediation: remediation}}
	}
	if issues := optionPair(osd, "$.spec.ceph.config.osd", []string{"osd_memory_target_autotune", "osd_memory_target"}, "fixed OSD memory declares osd_memory_target_autotune: \"false\" and osd_memory_target as a quoted integer-byte string, and no other option"); len(issues) != 0 {
		return nil, issues
	}
	autotune := osd.Get("osd_memory_target_autotune")
	if autotune.Type() != api.String || autotune.Text() != "false" {
		return refuse("$.spec.ceph.config.osd.osd_memory_target_autotune", "fixed OSD memory requires explicitly disabled host autotuning", "use the quoted string \"false\" with the declared manual memory target")
	}
	target := osd.Get("osd_memory_target")
	text := target.Text()
	bytes, err := strconv.ParseUint(text, 10, 64)
	if target.Type() != api.String || len(text) > 20 || err != nil || bytes < 2<<30 || strconv.FormatUint(bytes, 10) != text {
		return refuse("$.spec.ceph.config.osd.osd_memory_target", "the OSD target must be canonical integer bytes from 2147483648 through 18446744073709551615", "use a quoted decimal byte count, for example \"2147483648\"; qualify the aggregate daemon and host memory budget separately")
	}
	return map[string]string{"osd_memory_target_autotune": "false", "osd_memory_target": text}, nil
}

func corePoolDefaults(global api.Value) (map[string]string, []api.Issue) {
	refuse := func(field, message string) (map[string]string, []api.Issue) {
		return nil, []api.Issue{{Code: "api.invariant", Field: field, Message: message, Remediation: "declare osd_pool_default_size and osd_pool_default_min_size together as the quoted string \"2\"; omission preserves provider defaults"}}
	}
	keys := []string{"osd_pool_default_size", "osd_pool_default_min_size"}
	if issues := optionPair(global, "$.spec.ceph.config.global", keys, "core pool defaults declare osd_pool_default_size and osd_pool_default_min_size together as the quoted string \"2\", and no other option"); len(issues) != 0 {
		return nil, issues
	}
	for _, key := range keys {
		value := global.Get(key)
		if value.Type() != api.String || value.Text() != "2" {
			return refuse("$.spec.ceph.config.global."+key, "the core pool-default candidate admits only the native string \"2\"")
		}
	}
	return map[string]string{keys[0]: "2", keys[1]: "2"}, nil
}

var configOption = regexp.MustCompile(`^[a-z0-9_]{1,128}$`)

func optionPair(section api.Value, field string, keys []string, remediation string) []api.Issue {
	issues := []api.Issue{}
	for _, option := range section.Fields() {
		if slices.Contains(keys, option.Name) {
			continue
		}
		at, name := field, "the unqualified option"
		if configOption.MatchString(option.Name) {
			at, name = field+"."+option.Name, option.Name
		}
		issues = append(issues, api.Issue{Code: "api.invariant", Field: at, Message: "this config option is not qualified for the core profile", Remediation: "remove " + name + " from " + field + "; " + remediation})
	}
	for _, key := range keys {
		if !section.Has(key) {
			issues = append(issues, api.Issue{Code: "api.required", Field: field + "." + key, Message: "the qualified option pair requires both members", Remediation: "declare " + key + " in " + field + "; " + remediation})
		}
	}
	return issues
}
