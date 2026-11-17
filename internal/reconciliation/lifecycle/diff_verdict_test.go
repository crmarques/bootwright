package lifecycle

import (
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func verdictClient(access string) *api.Object {
	object := api.NewObject(api.CephClient, "rbd-reader", api.Value{}, api.MapValue(
		api.FieldValue{Name: "poolRef", Value: api.StringValue("rbd-data")},
		api.FieldValue{Name: "access", Value: api.StringValue(access)},
	))
	return &object
}

func verdictObject(labels api.Value, fields ...api.FieldValue) *api.Object {
	object := api.NewObject(api.CephClient, "rbd-reader", labels, api.MapValue(fields...))
	return &object
}

func TestJudgeMatchesTheEncodedDocumentComparison(t *testing.T) {
	t.Parallel()
	pool := api.FieldValue{Name: "poolRef", Value: api.StringValue("rbd-data")}
	access := api.FieldValue{Name: "access", Value: api.StringValue("readWrite")}
	cases := []struct {
		name          string
		desired, real *api.Object
		want          Difference
	}{
		{"the same fields in another order", verdictObject(api.Value{}, pool, access), verdictObject(api.Value{}, access, pool), DifferenceNone},
		{"absent labels against empty labels", verdictObject(api.Value{}, pool), verdictObject(api.MapValue(), pool), DifferenceChanged},
		{"an integer against a number of the same text", verdictObject(api.Value{}, api.FieldValue{Name: "size", Value: api.IntegerValue("1")}), verdictObject(api.Value{}, api.FieldValue{Name: "size", Value: api.NumberValue("1")}), DifferenceChanged},
		{"a nested difference", verdictObject(api.Value{}, api.FieldValue{Name: "caps", Value: api.MapValue(access)}), verdictObject(api.Value{}, api.FieldValue{Name: "caps", Value: api.MapValue(pool)}), DifferenceChanged},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result := &DiffResult{Objects: []ObjectDiff{{Desired: test.desired, Real: RealSide{State: "present", Object: test.real}}}}
			Judge(result)
			if got := result.Objects[0].Difference; got != test.want {
				t.Fatalf("difference = %q, want %q", got, test.want)
			}
		})
	}
}

func TestDiffDecidesDifferenceFromEncodedBytes(t *testing.T) {
	t.Parallel()
	client := verdictClient("readWrite")
	same := verdictClient("readWrite")
	other := verdictClient("readOnly")
	cases := []struct {
		name   string
		object ObjectDiff
		want   Difference
		state  string
	}{
		{"unknown wins", ObjectDiff{Desired: client, Real: RealSide{State: "unknown"}}, DifferenceUnknown, "unknown"},
		{"absent and declared", ObjectDiff{Desired: client, Real: RealSide{State: "absent"}}, DifferenceAdded, "absent"},
		{"present and undeclared", ObjectDiff{Real: RealSide{State: "present", Object: client}}, DifferenceRemoved, "present"},
		{"present, undeclared and discovered", ObjectDiff{Real: RealSide{State: "present", Discovered: true, Object: client}}, DifferenceUnmanaged, "present"},
		{"equal documents", ObjectDiff{Desired: client, Real: RealSide{State: "present", Object: same}}, DifferenceNone, "present"},
		{"other documents", ObjectDiff{Desired: other, Real: RealSide{State: "present", Object: client}}, DifferenceChanged, "present"},
		{"a present side with no document reads unknown", ObjectDiff{Desired: client, Real: RealSide{State: "present"}}, DifferenceUnknown, "unknown"},
		{"a partial side with no document reads unknown", ObjectDiff{Desired: client, Real: RealSide{State: "partial", Reason: "kept"}}, DifferenceUnknown, "unknown"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result := &DiffResult{Objects: []ObjectDiff{test.object}}
			Judge(result)
			if got := result.Objects[0]; got.Difference != test.want || got.Real.State != test.state {
				t.Fatalf("difference = %q on a %q side, want %q on %q", got.Difference, got.Real.State, test.want, test.state)
			}
		})
	}
}

func TestJudgeSummarizesAndDecidesWhetherTheStateDiffers(t *testing.T) {
	t.Parallel()
	client := verdictClient("readWrite")
	result := &DiffResult{Observed: true, Objects: []ObjectDiff{
		{Desired: client, Real: RealSide{Source: "applied", State: "present", Object: verdictClient("readWrite")}},
		{Desired: client, Real: RealSide{Source: "applied", State: "present", Object: verdictClient("readOnly")}},
		{Desired: client, Real: RealSide{Source: "observed", State: "absent"}},
		{Real: RealSide{Source: "applied", State: "present", Object: client}},
		{Real: RealSide{Source: "observed", State: "present", Discovered: true, Object: client}},
		{Desired: client, Real: RealSide{State: "unknown"}},
	}}
	Judge(result)
	want := DiffSummary{None: 1, Changed: 1, Added: 1, Removed: 1, Unmanaged: 1, Unknown: 1, Applied: 3}
	if result.Summary != want || !SummaryDiffers(result.Summary) {
		t.Fatalf("summary = %+v, want %+v and a difference", result.Summary, want)
	}
	quiet := &DiffResult{Objects: []ObjectDiff{{Desired: client, Real: RealSide{State: "present", Object: verdictClient("readWrite")}}}}
	Judge(quiet)
	if SummaryDiffers(quiet.Summary) || quiet.Summary.None != 1 {
		t.Fatalf("summary = %+v, want no difference", quiet.Summary)
	}
	if DifferenceDiffers(DifferenceNone) || DifferenceDiffers(DifferenceUnknown) || !DifferenceDiffers(DifferenceChanged) || !DifferenceDiffers(DifferenceUnmanaged) {
		t.Fatal("DifferenceDiffers disagrees with the verdict values")
	}
}
