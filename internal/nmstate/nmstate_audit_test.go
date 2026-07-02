package nmstate

import "testing"

// TestMergePropagatesNilForConflictingOverrides asserts the merge-drop guard
// this package exists to protect: Merge now returns mergo's error instead of
// discarding it, and for the inputs this package feeds mergo (a *map[string]any
// dst and a map[string]any src of identical type) that error is always nil even
// when nested scalar/map/slice types conflict — those overwrite under
// WithOverride. If a future change made Merge able to error here, EffectiveConfig
// would panic instead of silently dropping the merge; this test pins the guard to
// its no-error contract.
func TestMergePropagatesNilForConflictingOverrides(t *testing.T) {
	cases := []struct {
		name  string
		base  map[string]any
		patch map[string]any
	}{
		{"scalar-over-map", map[string]any{"k": map[string]any{"a": 1}}, map[string]any{"k": "s"}},
		{"map-over-scalar", map[string]any{"k": "s"}, map[string]any{"k": map[string]any{"a": 1}}},
		{"slice-over-scalar", map[string]any{"k": "s"}, map[string]any{"k": []any{1, 2}}},
		{"scalar-over-slice", map[string]any{"k": []any{1, 2}}, map[string]any{"k": "s"}},
		{"named-seq", map[string]any{"interfaces": []any{map[string]any{"name": "eth0", "mtu": 1500}}}, map[string]any{"interfaces": []any{map[string]any{"name": "eth0", "mtu": 9000}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Merge(tc.base, tc.patch); err != nil {
				t.Fatalf("Merge returned error for %s: %v", tc.name, err)
			}
		})
	}
}

// TestEffectiveConfigMergesOverridesWithoutPanic exercises the EffectiveConfig
// call site: the override merge lands (override wins on scalar conflict) and the
// unreachable-error panic guard never trips for representative inputs.
func TestEffectiveConfigMergesOverridesWithoutPanic(t *testing.T) {
	template := map[string]any{
		"interfaces": []any{
			map[string]any{"name": "eth0", "mtu": 1500, "ipv4": map[string]any{"enabled": true}},
		},
	}
	overrides := map[string]any{
		"interfaces": []any{
			map[string]any{"name": "eth0", "mtu": 9000},
		},
	}
	out := EffectiveConfig(template, overrides, nil)
	eth0 := out["interfaces"].([]any)[0].(map[string]any)
	if eth0["mtu"] != 9000 {
		t.Fatalf("override mtu not applied: %#v", eth0)
	}
	if eth0["ipv4"].(map[string]any)["enabled"] != true {
		t.Fatalf("template field lost during merge: %#v", eth0)
	}
}
