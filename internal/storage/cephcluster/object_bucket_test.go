package cephcluster

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func objectBucket(name string, pairs ...any) api.Object {
	spec := m("clusterRef", "storage", "objectGatewayRef", "objects", "ownerRef", "app-writer")
	for i := 0; i < len(pairs); i += 2 {
		spec = spec.With(pairs[i].(string), v(pairs[i+1]))
	}
	return object(api.CephObjectBucket, name, spec)
}

func bucketIssues(t *testing.T, bucket api.Object, catalog api.Catalog) []api.Issue {
	t.Helper()
	catalog = replace(replace(catalog, objectUser("app-writer")), bucket)
	return Validate(bucket, catalog)
}

func TestObjectBucketAdmitsAnOwnerOfItsGateway(t *testing.T) {
	catalog := gatewayCatalog(t)
	policy := m("Version", "2012-10-17", "Statement", []api.Value{m("Effect", "Allow", "Action", []api.Value{v("s3:GetObject")}, "Resource", []api.Value{v("arn:aws:s3:::app-data/*")})})
	for _, bucket := range []api.Object{objectBucket("app-data"), objectBucket("abc"), objectBucket(strings.Repeat("b", 44)), objectBucket("app-data", "policy", policy)} {
		if issues := bucketIssues(t, bucket, catalog); len(issues) != 0 {
			t.Fatalf("%s refused: %#v", bucket.Name(), issues)
		}
	}
}

func TestObjectBucketNameIsAnS3Name(t *testing.T) {
	exactlyOne(t, bucketIssues(t, objectBucket("ab"), gatewayCatalog(t)), "$.metadata.name", "an S3 bucket name has at least 3 characters",
		"lengthen metadata.name to at least 3 characters; it is the native bucket name")
	if issues := bucketIssues(t, objectBucket(strings.Repeat("b", 63)), gatewayCatalog(t)); len(issues) != 0 {
		t.Fatalf("a 63-character bucket name, the S3 maximum, was refused: %v", issues)
	}
}

func TestObjectBucketOwnerIsAUserOfItsGateway(t *testing.T) {
	catalog := replace(gatewayCatalog(t), objectUser("other-writer", "objectGatewayRef", "elsewhere"))
	bucket := objectBucket("app-data", "ownerRef", "other-writer")
	catalog = replace(catalog, bucket)
	exactlyOne(t, Validate(bucket, catalog), "$.spec.ownerRef",
		"CephObjectBucket app-data is owned by CephObjectUser other-writer of another CephObjectGateway, whose zone cannot hold this bucket",
		"set spec.ownerRef to a CephObjectUser whose spec.objectGatewayRef is objects, or set spec.objectGatewayRef to that user's gateway")
}

func TestObjectBucketRefusesAnOwnerOrGatewayOfAnotherCluster(t *testing.T) {
	gateway, catalog := editGateway(t, gatewayCatalog(t), "clusterRef", "other")
	catalog = replace(catalog, gateway)
	found := issuesAt(bucketIssues(t, objectBucket("app-data"), catalog), "$.spec.objectGatewayRef")
	if len(found) != 1 || found[0].Message != "referenced storage child belongs to a different cluster" {
		t.Fatalf("a gateway of another cluster: %#v", found)
	}
	catalog = replace(gatewayCatalog(t), objectUser("app-writer", "clusterRef", "other"))
	bucket := objectBucket("app-data")
	found = issuesAt(Validate(bucket, replace(catalog, bucket)), "$.spec.ownerRef")
	if len(found) != 1 || found[0].Message != "referenced storage child belongs to a different cluster" {
		t.Fatalf("an owner of another cluster: %#v", found)
	}
}

func TestObjectBucketPolicyIsANativePolicyDocument(t *testing.T) {
	statement := []api.Value{m("Effect", "Allow")}
	const message = "CephObjectBucket app-data declares a policy without a Version string or without a Statement holding at least one statement, which the native IAM policy grammar requires and which RGW would store as a policy that grants nothing"
	const remedy = "write spec.policy as a mapping with a Version string, such as 2012-10-17, and a Statement holding one mapping or a list of at least one"
	for name, policy := range map[string]api.Value{
		"empty document":      m(),
		"no statement":        m("Version", "2012-10-17"),
		"no version":          m("Statement", statement),
		"empty list":          m("Version", "2012-10-17", "Statement", []api.Value{}),
		"empty mapping":       m("Version", "2012-10-17", "Statement", m()),
		"statement is a text": m("Version", "2012-10-17", "Statement", "Allow"),
		"statement is a flag": m("Version", "2012-10-17", "Statement", true),
		"version is a number": m("Version", 2012, "Statement", statement),
		"version is empty":    m("Version", "", "Statement", statement),
		"a text":              v("Allow"),
		"a list":              v(statement),
	} {
		t.Run(name, func(t *testing.T) {
			exactlyOne(t, bucketIssues(t, objectBucket("app-data", "policy", policy), gatewayCatalog(t)), "$.spec.policy", message, remedy)
		})
	}
	for name, policy := range map[string]api.Value{
		"one statement":  m("Version", "2012-10-17", "Statement", m("Effect", "Allow")),
		"a list":         m("Version", "2012-10-17", "Statement", statement),
		"an old version": m("Version", "2008-10-17", "Statement", statement, "Id", "p"),
	} {
		if found := issuesAt(bucketIssues(t, objectBucket("app-data", "policy", policy), gatewayCatalog(t)), "$.spec.policy"); len(found) != 0 {
			t.Fatalf("%s refused: %#v", name, found)
		}
	}
}
