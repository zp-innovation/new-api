package workbuddy

import (
	"regexp"
	"testing"
	"time"
)

// The expected values are the reference gateway's own outputs: they were
// produced with hashlib.md5(("<salt>:<uid>").encode()).hexdigest() and must
// never change, because the upstream sees them as this account's fixed device.
func TestDerivedDeviceIDsMatchReferenceGoldenValues(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{name: "machine for uid-1", got: MachineID("uid-1"), want: "1b196e499e3cd32e36358cb5450a9914"},
		{name: "session for uid-1", got: SessionID("uid-1"), want: "b36ebcd6e8b51c8e0b87ac2f5fe46ea9"},
		{name: "machine for empty uid", got: MachineID(""), want: "fc1f3f603c8bcdc0783942ea0169ab1e"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

func TestDerivedIDsAreStableAndAccountScoped(t *testing.T) {
	if MachineID("uid-1") != MachineID("uid-1") {
		t.Error("MachineID is not stable for one uid")
	}
	if MachineID("uid-1") == MachineID("uid-2") {
		t.Error("two uids share one machine id")
	}
	if SessionID("uid-1") == MachineID("uid-1") {
		t.Error("machine and session ids must not collide")
	}
	if MachineID("") != MachineID("anonymous") {
		t.Error("an empty uid must fall back to the anonymous identity")
	}

	hex32 := regexp.MustCompile("^[0-9a-f]{32}$")
	for _, id := range []string{MachineID("uid-1"), SessionID("uid-1"), MachineID("")} {
		if !hex32.MatchString(id) {
			t.Errorf("derived id %q is not 32 lowercase hex characters", id)
		}
	}
}

func TestRequestIDCarriesDerivedPrefixAndMicroseconds(t *testing.T) {
	if got, want := RequestID("uid-1", time.Unix(0, 123456789)), "f2845c7ef2a8a8d654f1c55eed4d5730-456789"; got != want {
		t.Errorf("RequestID: got %q, want %q", got, want)
	}

	pattern := regexp.MustCompile("^[0-9a-f]{32}-[0-9]{6}$")
	for _, nanos := range []int64{0, 1, 999999, 123456789, 86400000000123} {
		if got := RequestID("uid-1", time.Unix(0, nanos)); !pattern.MatchString(got) {
			t.Errorf("RequestID for %d ns is %q, want 32 hex digits, a dash and 6 digits", nanos, got)
		}
	}

	if RequestID("uid-1", time.Unix(0, 5)) == RequestID("uid-2", time.Unix(0, 5)) {
		t.Error("two uids share one request-id prefix")
	}
}
