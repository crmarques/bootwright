package v1alpha1

func machineClearSchema() *Shape {
	target := record(field("deviceName", lexical("device-path")), field("wwn", nonempty()), required("serialNumber", nonempty()), required("sizeBytes", integer("4096", "70368744177664")))
	return record(required("targets", nonemptyArray(&Shape{Type: Sequence, Atomic: true, NameKey: "serialNumber", Element: target})))
}
