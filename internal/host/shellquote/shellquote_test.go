package shellquote

import "testing"

func TestQuote(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"empty arg", []string{"", "x"}, "'' x"},
		{"plain", []string{"ansible-playbook", "-i", "inv"}, "ansible-playbook -i inv"},
		{"space-containing", []string{"echo", "hello world"}, "echo 'hello world'"},
		{"with-quotes", []string{"foo", "a'b"}, "foo 'a'\\''b'"},
		{"with-dollar", []string{"sh", "-c", "$X"}, "sh -c '$X'"},
		{"with-tab", []string{"x\ty"}, "'x\ty'"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Quote(tc.in); got != tc.want {
				t.Fatalf("Quote(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
