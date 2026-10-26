package antigravity

import "testing"

func TestNormalizeUserAgentVersion(t *testing.T) {
	valid := []string{"2.9.1", " 1.0.0 ", "10.20.30"}
	for _, version := range valid {
		if got := NormalizeUserAgentVersion(version); got == "" {
			t.Errorf("NormalizeUserAgentVersion(%q) = \"\", want a value", version)
		}
	}

	invalid := []string{"", "   ", "2.9", "v2.9.1", "2.9.1.4", "latest", "2.9.x"}
	for _, version := range invalid {
		if got := NormalizeUserAgentVersion(version); got != "" {
			t.Errorf("NormalizeUserAgentVersion(%q) = %q, want \"\"", version, got)
		}
	}
}

func TestBuildUserAgent(t *testing.T) {
	if got, want := BuildUserAgent("2.9.1"), "antigravity/hub/2.9.1 windows/amd64"; got != want {
		t.Errorf("BuildUserAgent(2.9.1) = %q, want %q", got, want)
	}
	// An invalid version must fall back to the configured default instead of
	// emitting a malformed header upstream.
	want := "antigravity/hub/" + DefaultVersion() + " windows/amd64"
	if got := BuildUserAgent("not-a-version"); got != want {
		t.Errorf("BuildUserAgent(invalid) = %q, want %q", got, want)
	}
}

func TestDefaultUserAgentVersionMatchesUpstreamClient(t *testing.T) {
	if DefaultUserAgentVersion != "2.23.0" {
		t.Fatalf("DefaultUserAgentVersion = %q, want the client version the upstream now expects (2.23.0)", DefaultUserAgentVersion)
	}
}

func TestDefaultVersionIsValid(t *testing.T) {
	if NormalizeUserAgentVersion(DefaultVersion()) == "" {
		t.Fatalf("default version %q is not a valid dotted version", DefaultVersion())
	}
}
