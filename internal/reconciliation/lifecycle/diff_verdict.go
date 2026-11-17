package lifecycle

import (
	"cmp"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

type Difference string

const (
	DifferenceNone      Difference = "none"
	DifferenceChanged   Difference = "changed"
	DifferenceAdded     Difference = "added"
	DifferenceRemoved   Difference = "removed"
	DifferenceUnmanaged Difference = "unmanaged"
	DifferenceUnknown   Difference = "unknown"
)

func DifferenceDiffers(d Difference) bool {
	return d != DifferenceNone && d != DifferenceUnknown
}

type DiffSummary struct {
	None, Changed, Added, Removed, Unmanaged, Unknown, Applied int
}

func SummaryDiffers(s DiffSummary) bool {
	return s.Changed+s.Added+s.Removed+s.Unmanaged+s.Unknown != 0
}

func Judge(r *DiffResult) {
	r.Summary = DiffSummary{}
	for index := range r.Objects {
		object := &r.Objects[index]
		object.Real = documented(object.Real)
		object.Difference = judge(*object)
		r.Summary.count(*object, r.Observed)
	}
}

func documented(side RealSide) RealSide {
	if (side.State != sidePresent && side.State != sidePartial) || side.Object != nil {
		return side
	}
	side.State, side.Reason = sideUnknown, cmp.Or(side.Reason, "the real side holds no document to show")
	return side
}

func judge(object ObjectDiff) Difference {
	switch {
	case object.Real.State == sideUnknown:
		return DifferenceUnknown
	case object.Real.State == sideAbsent && object.Desired != nil:
		return DifferenceAdded
	case object.Desired == nil && object.Real.State != sideAbsent && object.Real.Discovered:
		return DifferenceUnmanaged
	case object.Desired == nil && object.Real.State != sideAbsent:
		return DifferenceRemoved
	case equalObjects(object.Real.Object, object.Desired):
		return DifferenceNone
	}
	return DifferenceChanged
}

func equalObjects(real, desired *api.Object) bool {
	if real == nil || desired == nil {
		return real == desired
	}
	return real.Identity() == desired.Identity() && real.Value().Equal(desired.Value())
}

func (s *DiffSummary) count(object ObjectDiff, observed bool) {
	switch object.Difference {
	case DifferenceChanged:
		s.Changed++
	case DifferenceAdded:
		s.Added++
	case DifferenceRemoved:
		s.Removed++
	case DifferenceUnmanaged:
		s.Unmanaged++
	case DifferenceUnknown:
		s.Unknown++
	default:
		s.None++
	}
	if observed && object.Real.Source == sideApplied && (object.Real.State == sidePresent || object.Real.State == sidePartial) {
		s.Applied++
	}
}
