package v1alpha1

func machineEraseSchema() *Shape {
	target := record(required("deviceName", lexical("device-path")), required("wwn", nonempty()), required("serialNumber", nonempty()), required("sizeBytes", integer("4096", "70368744177664")))
	return record(required("method", enumeration("logicalOverwrite")), required("targets", nonemptyArray(&Shape{Type: Sequence, Atomic: true, NameKey: "deviceName", Element: target})))
}
