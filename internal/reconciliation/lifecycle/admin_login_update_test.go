package lifecycle

import (
	"encoding/json"
	"strings"
	"testing"
)

func pendingLogin(change func(*loginUpdate)) []byte {
	update := loginUpdate{Guard: json.RawMessage(`{"generation":1}`), Password: "visible-password", Previous: strings.Repeat("ab", 32), User: "admin"}
	if change != nil {
		change(&update)
	}
	encoded, _ := json.Marshal(update)
	return encoded
}

// A retained login update is trusted only when it is closed, small and well
// formed: a previous digest of 64 hex digits, a JSON guard, and a user and
// password of visible characters that no markup or query separator can
// reshape.
func TestALoginUpdateDecodesOnlyAWellFormedPendingValue(t *testing.T) {
	var accepted loginUpdate
	if err := decodeLoginUpdate(pendingLogin(nil), &accepted); err != nil || accepted.User != "admin" || accepted.Password != "visible-password" {
		t.Fatalf("a well-formed value gave %+v, %v", accepted, err)
	}
	for name, value := range map[string][]byte{
		"oversized":            []byte(strings.Repeat(" ", 16385)),
		"not json":             []byte("pending"),
		"an unknown field":     []byte(`{"guard":{},"password":"p","previous":"` + strings.Repeat("ab", 32) + `","user":"u","extra":1}`),
		"a short previous":     pendingLogin(func(u *loginUpdate) { u.Previous = "abcd" }),
		"a non-hex previous":   pendingLogin(func(u *loginUpdate) { u.Previous = strings.Repeat("zz", 32) }),
		"no guard":             []byte(`{"password":"p","previous":"` + strings.Repeat("ab", 32) + `","user":"u"}`),
		"a guard past 4096":    pendingLogin(func(u *loginUpdate) { u.Guard = json.RawMessage(`"` + strings.Repeat("g", 4096) + `"`) }),
		"an empty user":        pendingLogin(func(u *loginUpdate) { u.User = "" }),
		"a spaced password":    pendingLogin(func(u *loginUpdate) { u.Password = "has space" }),
		"a markup password":    pendingLogin(func(u *loginUpdate) { u.Password = "a<b" }),
		"an ampersand user":    pendingLogin(func(u *loginUpdate) { u.User = "a&b" }),
		"a non-ASCII password": pendingLogin(func(u *loginUpdate) { u.Password = "pässword" }),
		"a password too long":  pendingLogin(func(u *loginUpdate) { u.Password = strings.Repeat("p", maxVisibleValue+1) }),
	} {
		var update loginUpdate
		if err := decodeLoginUpdate(value, &update); err == nil {
			t.Errorf("%s was accepted as %+v", name, update)
		}
	}
}

// An administrator update is valid only with a previous digest, a JSON guard, a
// visible password of 23 to 64 characters, and a hash the executable's own
// matcher proves the password has; a build without a matcher trusts none.
func TestAnAdminUpdateIsValidOnlyWithAHashThePasswordMatches(t *testing.T) {
	valid := adminUpdate{Previous: strings.Repeat("ab", 32), Guard: json.RawMessage(`{"generation":1}`), Password: strings.Repeat("p", 23), Hash: "hash-of-password"}
	matching := AdminCredentialService{passwordMatches: func(password, hash []byte) bool {
		return string(password) == strings.Repeat("p", 23) && string(hash) == "hash-of-password"
	}}
	if !matching.validAdminUpdate(valid) {
		t.Fatal("a well-formed update was refused")
	}
	for name, change := range map[string]func(*adminUpdate){
		"a short previous":    func(u *adminUpdate) { u.Previous = "abcd" },
		"a non-hex previous":  func(u *adminUpdate) { u.Previous = strings.Repeat("zz", 32) },
		"a short password":    func(u *adminUpdate) { u.Password = strings.Repeat("p", 22) },
		"a long password":     func(u *adminUpdate) { u.Password = strings.Repeat("p", 65) },
		"a spaced password":   func(u *adminUpdate) { u.Password = strings.Repeat("p", 22) + " " },
		"no guard":            func(u *adminUpdate) { u.Guard = nil },
		"a guard that is not": func(u *adminUpdate) { u.Guard = json.RawMessage(`{`) },
		"a foreign hash":      func(u *adminUpdate) { u.Hash = "hash-of-another" },
	} {
		update := valid
		change(&update)
		if matching.validAdminUpdate(update) {
			t.Errorf("%s was accepted", name)
		}
	}
	if (AdminCredentialService{}).validAdminUpdate(valid) {
		t.Error("an update was trusted without a password matcher")
	}
}
