package material

import "testing"

func TestAdminPasswordHashMatchesOnlyItsPasswordAndBoundsTheCost(t *testing.T) {
	password := []byte("synthetic-console-password-12345")
	hash, err := BcryptPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(hash)
	if !BcryptPasswordMatches(password, hash) || BcryptPasswordMatches([]byte("different-password"), hash) {
		t.Fatal("password comparison did not preserve its identity")
	}
	expensive := append([]byte(nil), hash...)
	expensive[4], expensive[5] = '3', '1'
	if BcryptPasswordMatches(password, expensive) {
		t.Fatal("unbounded hash cost was accepted")
	}
	clear(expensive)
}
