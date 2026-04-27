package cli

import "testing"

// TestIsDevBuild covers the dev-build detection used by the hira CLI
// and daemon to refuse self-update when running a non-released binary.
// The regex is intentionally strict: only `vMAJOR.MINOR.PATCH` (matching
// what GitHub publishes as a release tag) is treated as a release.
func TestIsDevBuild(t *testing.T) {
	cases := []struct {
		name    string
		version string
		want    bool
	}{
		{"clean release tag", "v0.2.16", false},
		{"two-digit major", "v10.3.1", false},
		{"zero patch", "v1.0.0", false},
		{"trimmable whitespace around release", "  v0.2.16  ", false},

		{"dirty tree marker", "v0.2.16-dirty", true},
		{"git describe past tag", "v0.2.16-3-gabc1234", true},
		{"bare commit sha", "f6df395", true},
		{"default ldflag literal", "dev", true},
		{"empty string", "", true},
		{"only whitespace", "   ", true},
		{"missing v prefix", "0.2.16", true},
		{"prerelease suffix", "v0.3.0-rc1", true},
		{"four segments", "v0.2.16.1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsDevBuild(tc.version); got != tc.want {
				t.Errorf("IsDevBuild(%q) = %v, want %v", tc.version, got, tc.want)
			}
		})
	}
}
