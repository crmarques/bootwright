package render

import "github.com/crmarques/bootwright/api/v1alpha1"

// EffectiveState returns the state shape emitted under
// <rendered-dir>/effective-state.yaml. The effective state is exactly the
// already-normalized, defaults-applied state the renderer is handed — there is
// no renderer-level overlay step — so this is an identity marker: it names the
// single point every write path funnels the snapshot through, so any future
// effective-only transform has one place to live.
func EffectiveState(state v1alpha1.State) v1alpha1.State { return state }
