package storage

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
	return object(api.StorageObjectBucket, name, spec)
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

func TestObjectBucketNameIsAnS3NameThatFitsItsBlock(t *testing.T) {
	exactlyOne(t, bucketIssues(t, objectBucket("ab"), gatewayCatalog(t)), "$.metadata.name", "an S3 bucket name has at least 3 characters",
		"lengthen metadata.name to at least 3 characters; it is the native bucket name")
	exactlyOne(t, bucketIssues(t, objectBucket(strings.Repeat("b", 45)), gatewayCatalog(t)), "$.metadata.name",
		"an object bucket name longer than 44 characters does not fit its block identity ceph-object-bucket-<name> of at most 63 characters",
		"shorten metadata.name to at most 44 characters")
}

func TestObjectBucketOwnerIsAUserOfItsGateway(t *testing.T) {
	catalog := replace(gatewayCatalog(t), objectUser("other-writer", "objectGatewayRef", "elsewhere"))
	bucket := objectBucket("app-data", "ownerRef", "other-writer")
	catalog = replace(catalog, bucket)
	exactlyOne(t, Validate(bucket, catalog), "$.spec.ownerRef",
		"StorageObjectBucket app-data is owned by StorageObjectUser other-writer of another StorageObjectGateway, whose zone cannot hold this bucket",
		"set spec.ownerRef to a StorageObjectUser whose spec.objectGatewayRef is objects, or set spec.objectGatewayRef to that user's gateway")
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
