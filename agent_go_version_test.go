package ngrok

import "testing"

func TestGoTrustsCompromisedEMudhraCAs(t *testing.T) {
	for _, tc := range []struct {
		v    string
		want bool
	}{
		{"go1.25.7", true},
		{"go1.26.9", true},
		{"go1.26.10", false},
		{"go1.26.11", false},
		{"go1.27rc1", true},
		{"go1.27.0", true},
		{"go1.27.2", true},
		{"go1.27.3", false},
		{"go1.28.0", false},
		{"devel go1.28-abcdef", false},
	} {
		if got := goTrustsCompromisedEMudhraCAs(tc.v); got != tc.want {
			t.Errorf("goTrustsCompromisedEMudhraCAs(%q) = %v, want %v", tc.v, got, tc.want)
		}
	}
}
