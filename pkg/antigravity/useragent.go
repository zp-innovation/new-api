package antigravity

import (
	"os"
	"regexp"
	"strings"
)

// DefaultUserAgentVersion is the Antigravity client version used when no
// override is configured.
const DefaultUserAgentVersion = "2.23.0"

// UserAgentVersionEnv overrides DefaultUserAgentVersion at process start.
const UserAgentVersionEnv = "ANTIGRAVITY_USER_AGENT_VERSION"

var userAgentVersionPattern = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// defaultUserAgentVersion is resolved once at start-up so a malformed
// environment override degrades to the built-in default instead of producing
// an upstream-rejected User-Agent on every request.
var defaultUserAgentVersion = func() string {
	if version := NormalizeUserAgentVersion(os.Getenv(UserAgentVersionEnv)); version != "" {
		return version
	}
	return DefaultUserAgentVersion
}()

// NormalizeUserAgentVersion validates a version string and returns "" when it
// is empty or not a dotted three-part version.
func NormalizeUserAgentVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" || !userAgentVersionPattern.MatchString(version) {
		return ""
	}
	return version
}

// BuildUserAgent renders the upstream User-Agent. An invalid version falls back
// to the configured default rather than emitting a malformed header.
func BuildUserAgent(version string) string {
	normalized := NormalizeUserAgentVersion(version)
	if normalized == "" {
		normalized = defaultUserAgentVersion
	}
	return "antigravity/hub/" + normalized + " windows/amd64"
}

// UserAgent returns the User-Agent for the configured default version.
func UserAgent() string {
	return BuildUserAgent(defaultUserAgentVersion)
}

// DefaultVersion returns the effective default client version.
func DefaultVersion() string {
	return defaultUserAgentVersion
}
