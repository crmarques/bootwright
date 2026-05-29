package render

import "github.com/crmarques/bootwright/api/v1alpha1"

// EffectiveState returns the state shape emitted under
// <rendered-dir>/effective-state.yaml after renderer-level overlays have
// been applied by the caller.
func EffectiveState(state v1alpha1.State) v1alpha1.State { return state }
