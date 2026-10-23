package v1alpha1

type FDFConfig struct {
	Subscription        PackageSubscription `json:"subscription"`
	StorageExportRef    string              `json:"storageExportRef"`
	DefaultStorageClass bool                `json:"defaultStorageClass"`
	Timeout             string              `json:"timeout"`
}

func fdfPackageSchema() *Shape {
	sub := packageSubscriptionSchema()
	for i := range sub.Fields {
		if sub.Fields[i].Name == "source" {
			sub.Fields[i].Required = true
		}
	}
	s := record(required("subscription", sub), required("storageExportRef", ref(StorageExport)), defaulted("defaultStorageClass", boolean(), BoolValue(false)), defaulted("timeout", duration(), StringValue("1h")))
	return s
}
