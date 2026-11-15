package v1alpha1

type FDFConfig struct {
	MultiCloudGateway   bool                `json:"multiCloudGateway"`
	Subscription        PackageSubscription `json:"subscription"`
	CephConsumerRef     string              `json:"cephConsumerRef"`
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
	s := record(required("subscription", sub), required("cephConsumerRef", ref(CephConsumer)), defaulted("defaultStorageClass", boolean(), BoolValue(false)), defaulted("multiCloudGateway", boolean(), BoolValue(true)), defaulted("timeout", duration(), StringValue("1h")))
	return s
}
