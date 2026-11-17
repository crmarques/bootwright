package v1alpha1

import "math/big"

func (s *Shape) ForValue(value Value) *Shape {
	for _, alternative := range s.Alternatives {
		if alternative.Type == value.Type() || alternative.Type == Number && value.Type() == Integer {
			return alternative
		}
	}
	return s
}

func (s *Shape) Admits(value Value) bool {
	return s.Type == value.Type() || s.Type == Number && value.Type() == Integer
}

func (s *Shape) OutsideRange(value Value) (below, above, numeric bool) {
	n, numeric := new(big.Rat).SetString(value.Text())
	if !numeric {
		return false, false, false
	}
	if limit, valid := new(big.Rat).SetString(s.Minimum); s.Minimum != "" && valid && n.Cmp(limit) < 0 {
		below = true
	}
	if limit, valid := new(big.Rat).SetString(s.Maximum); s.Maximum != "" && valid && n.Cmp(limit) > 0 {
		above = true
	}
	return below, above, true
}

func (s *Shape) ShortString(value Value) bool {
	return s.MinLength > 0 && value.Type() == String && len(value.Text()) < s.MinLength
}
