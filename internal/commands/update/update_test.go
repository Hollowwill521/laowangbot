package update

import "testing"

func TestNewerOnlyAcceptsNewerStableRelease(t *testing.T) {
	tests := []struct {
		name            string
		current, latest string
		want            bool
	}{
		{"patch upgrade", "0.2.0", "v0.2.1", true},
		{"minor upgrade", "v0.1.9", "v0.2.0", true},
		{"same version", "0.2.1", "v0.2.1", false},
		{"downgrade", "0.2.1", "v0.2.0", false},
		{"prerelease", "0.2.0", "v0.2.1-rc1", false},
		{"invalid current", "development", "v0.2.1", false},
		{"dev current", "dev", "v0.2.1", true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := newer(test.current, test.latest); got != test.want {
				t.Fatalf("newer(%q, %q) = %t, want %t", test.current, test.latest, got, test.want)
			}
		})
	}
}

func TestParseVersionRejectsOverflow(t *testing.T) {
	if _, ok := parseVersion("999999999999999999999999.0.0"); ok {
		t.Fatal("parseVersion accepted an overflowing component")
	}
}
