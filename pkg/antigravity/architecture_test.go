package antigravity

import (
	"os/exec"
	"strings"
	"testing"
)

// packagePath is this package's own import path; it is the only allowed
// new-api-prefixed entry in the dependency list.
const packagePath = "github.com/QuantumNous/new-api/pkg/antigravity"

// TestPackageHasNoHostImports enforces the layering rule from the integration
// plan: the protocol core must not depend on new-api packages, so an upstream
// refactor cannot break it and only the adaptor layer needs review on merge.
func TestPackageHasNoHostImports(t *testing.T) {
	output, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Skipf("go list is unavailable: %v", err)
	}

	for _, dep := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		dep = strings.TrimSpace(dep)
		if dep == "" || dep == packagePath {
			continue
		}
		if strings.HasPrefix(dep, "github.com/QuantumNous/new-api/") {
			t.Errorf("pkg/antigravity must not depend on host package %s", dep)
		}
	}
}
