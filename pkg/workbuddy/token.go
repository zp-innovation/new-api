package workbuddy

import (
	"encoding/base64"
	"encoding/json"
	"github.com/QuantumNous/new-api/common"
	"strings"
	"time"
)

// jwtClaims is the subset of the access token's claims this package relies on.
// The upstream issues Keycloak-style tokens: the account uid is the subject and
// exp is the only refresh deadline the hub trusts (wb_accounts.py:228-235, :1248).
type jwtClaims struct {
	Exp          int64  `json:"exp"`
	Sub          string `json:"sub"`
	Iss          string `json:"iss"`
	EnterpriseID string `json:"enterprise_id"`
}

// ParseJWTClaims decodes the payload segment of a JWT without verifying the
// signature. Nothing here is a security boundary: the token arrived over TLS
// from the upstream and is only read for identity and expiry.
func ParseJWTClaims(token string) jwtClaims {
	segments := strings.Split(strings.TrimSpace(token), ".")
	if len(segments) < 2 {
		return jwtClaims{}
	}
	segment := segments[1]
	if padding := len(segment) % 4; padding != 0 {
		segment += strings.Repeat("=", 4-padding)
	}
	payload, err := base64.URLEncoding.DecodeString(segment)
	if err != nil {
		return jwtClaims{}
	}
	var claims jwtClaims
	if err := common.Unmarshal(payload, &claims); err != nil {
		return jwtClaims{}
	}
	return claims
}

// TokenSubject returns the "sub" claim, i.e. the account uid, or "".
func TokenSubject(token string) string { return ParseJWTClaims(token).Sub }

// TokenEnterpriseID returns the "enterprise_id" claim. An account that switched
// to a team space carries its enterprise id only in the token, not in the
// account file (wb_accounts.py:1803-1818).
func TokenEnterpriseID(token string) string { return ParseJWTClaims(token).EnterpriseID }

// TokenExpiry returns the "exp" claim as a UTC time, or the zero time.
func TokenExpiry(token string) time.Time {
	exp := ParseJWTClaims(token).Exp
	if exp <= 0 {
		return time.Time{}
	}
	return time.Unix(exp, 0).UTC()
}

// NormalizeEpoch converts the upstream's expiresAt value (epoch seconds, epoch
// milliseconds or a numeric string) into a UTC time; anything unusable yields
// the zero time, like normalize_epoch (wb_accounts.py:240-247).
func NormalizeEpoch(value any) time.Time {
	number := 0.0
	switch typed := value.(type) {
	case float64:
		number = typed
	case int64:
		number = float64(typed)
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return time.Time{}
		}
		number = parsed
	case string:
		parsed, err := json.Number(strings.TrimSpace(typed)).Float64()
		if err != nil {
			return time.Time{}
		}
		number = parsed
	default:
		return time.Time{}
	}
	if number <= 0 {
		return time.Time{}
	}
	// A seconds value beyond 1e11 is really milliseconds.
	if number > 1e11 {
		number /= 1000.0
	}
	return time.Unix(int64(number), 0).UTC()
}
