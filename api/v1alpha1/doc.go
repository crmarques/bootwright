// Package v1alpha1 defines Bootwright public desired-state API structs.
//
// Reference fields carry the Ref/Refs suffix and are authored as plain name
// strings (see LocalObjectReference). One deliberate exception: Environment
// spec.containerClusters and spec.storageClusters are fleet selection lists,
// not references, so they stay plain strings without the Ref suffix.
package v1alpha1
