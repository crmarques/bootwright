package reconciliation

import "testing"

func TestThawRefusesAClosingDelimiterAfterTheRequest(t *testing.T) {
	type request struct {
		Version string `json:"version"`
	}
	if _, err := Thaw[request]([]byte(`{"version":"v1"}`), "fixture"); err != nil {
		t.Fatal(err)
	}
	for _, closer := range []string{"}", "]"} {
		if _, err := Thaw[request]([]byte(`{"version":"v1"}`+closer), "fixture"); err == nil {
			t.Errorf("a request followed by %s thawed", closer)
		}
	}
}
