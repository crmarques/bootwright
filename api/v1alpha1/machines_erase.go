package v1alpha1

func machineEraseSchema() *Shape {
	target := record(required("deviceName", lexical("device-path")), required("wwn", nonempty()), required("serialNumber", nonempty()), required("sizeBytes", integer("4096", "70368744177664")), field("method", eraseMethod()))
	return record(required("method", eraseMethod()), required("targets", nonemptyArray(&Shape{Type: Sequence, Atomic: true, NameKey: "deviceName", Element: target})))
}

func eraseMethod() *Shape { return enumeration("logicalOverwrite", "metadataClear") }
