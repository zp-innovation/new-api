package antigravity

import "encoding/json"

// This package is the Antigravity protocol core and must not depend on host
// packages; TestPackageHasNoHostImports in architecture_test.go enforces that
// with `go list -deps`. It therefore cannot reach the host JSON codec in
// common, which is where every other package of the root module goes through.
//
// These two wrappers are the only place in this package that touches
// encoding/json, so protocol logic carries no encoder calls and the layering
// exemption has exactly one documented home. They deliberately keep the
// standard encoder: the envelope, credential and tier shapes below are part of
// the upstream wire contract, not of the host's pluggable encoding pipeline.
func marshalJSON(value any) ([]byte, error) {
	return json.Marshal(value)
}

func unmarshalJSON(data []byte, target any) error {
	return json.Unmarshal(data, target)
}
