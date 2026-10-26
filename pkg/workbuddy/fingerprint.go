// Package workbuddy implements the wire protocol of the WorkBuddy (Tencent
// CodeBuddy) upstream: device identity, outbound identity headers, endpoint and
// constant tables. It is a pure protocol layer: it must not import any other
// package of this repository.
package workbuddy

import (
	// #nosec G501 -- md5 reproduces the reference gateway's device-id
	// derivation (wb_fingerprint.py:11-17); it is an identifier scheme, not a
	// security primitive.
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"time"
)

// Device-identity salts. These strings are part of the wire contract: the
// reference gateway derives the values it sends with exactly these salts, so
// changing one changes every derived identifier.
const (
	saltMachine = "machine"
	saltSession = "session"
	saltRequest = "req"
	// idLength mirrors the reference's [:36] truncation. An md5 hex digest is
	// only 32 characters, so the truncation is a no-op there as well; the
	// guard exists so the code never slices past the digest length.
	idLength = 36
)

// deriveID reproduces the reference gateway's stable pseudo-device derivation:
// md5("<salt>:<uid>") in lowercase hex, truncated to 36 characters.
//
// The reference writes hashlib.md5(seed).hexdigest()[:36] (wb_fingerprint.py:11-17).
// An md5 hex digest is 32 characters, so that truncation is a no-op in Python
// and the full digest is what the upstream actually sees; the length guard here
// keeps the output byte-identical to the reference without panicking.
// An empty uid falls back to the literal "anonymous", as in the reference.
func deriveID(uid, salt string) string {
	if uid == "" {
		uid = "anonymous"
	}
	sum := md5.Sum([]byte(salt + ":" + uid))
	digest := hex.EncodeToString(sum[:])
	if len(digest) > idLength {
		digest = digest[:idLength]
	}
	return digest
}

// MachineID returns the stable per-account device code sent as X-Machine-ID.
//
// It is derived locally from the account uid; the upstream never issues it.
// The same uid always yields the same value, which is the point: the account's
// requests keep looking like they come from one fixed machine.
func MachineID(uid string) string { return deriveID(uid, saltMachine) }

// SessionID returns the stable per-account session identifier sent as
// X-Session-ID. Same derivation and same stability guarantee as MachineID.
func SessionID(uid string) string { return deriveID(uid, saltSession) }

// RequestID returns the X-Request-ID used on the billing path: a derived
// stable prefix plus a zero-padded microsecond suffix
// (wb_fingerprint.py:20-24, wb_accounts.py:1119).
func RequestID(uid string, now time.Time) string {
	return fmt.Sprintf("%s-%06d", deriveID(uid, saltRequest), now.UnixNano()%1_000_000)
}
