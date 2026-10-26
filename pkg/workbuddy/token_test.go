package workbuddy

import (
	"encoding/base64"
	"testing"
	"time"
)

func TestParseJWTClaims(t *testing.T) {
	token := testJWT(t, "uid-42", 1893456000)
	claims := ParseJWTClaims(token)
	if claims.Sub != "uid-42" {
		t.Errorf("sub: got %q", claims.Sub)
	}
	if claims.Exp != 1893456000 {
		t.Errorf("exp: got %d", claims.Exp)
	}
	if got := TokenSubject(token); got != "uid-42" {
		t.Errorf("TokenSubject: got %q", got)
	}
	if got, want := TokenExpiry(token), time.Unix(1893456000, 0).UTC(); !got.Equal(want) {
		t.Errorf("TokenExpiry: got %s, want %s", got, want)
	}
}

func TestParseJWTClaimsToleratesGarbage(t *testing.T) {
	for _, token := range []string{"", "not-a-jwt", "a.b", "a." + base64.RawURLEncoding.EncodeToString([]byte("not json")) + ".c"} {
		claims := ParseJWTClaims(token)
		if claims.Sub != "" || claims.Exp != 0 {
			t.Errorf("ParseJWTClaims(%q) = %+v, want empty claims", token, claims)
		}
		if !TokenExpiry(token).IsZero() {
			t.Errorf("TokenExpiry(%q) must be the zero time", token)
		}
	}
}

func TestParseJWTClaimsAcceptsPaddedPayload(t *testing.T) {
	payload := base64.URLEncoding.EncodeToString([]byte(`{"sub":"uid-1"}`))
	token := "header." + payload + ".signature"
	if got := TokenSubject(token); got != "uid-1" {
		t.Errorf("TokenSubject with padded payload: got %q", got)
	}
}

func TestNormalizeEpoch(t *testing.T) {
	want := time.Unix(1900000000, 0).UTC()
	cases := []struct {
		name  string
		value any
		want  time.Time
	}{
		{name: "epoch seconds as float64", value: float64(1900000000), want: want},
		{name: "epoch seconds as string", value: "1900000000", want: want},
		{name: "epoch milliseconds", value: float64(1900000000000), want: want},
		{name: "zero", value: float64(0), want: time.Time{}},
		{name: "negative", value: float64(-5), want: time.Time{}},
		{name: "nil", value: nil, want: time.Time{}},
		{name: "text", value: "yesterday", want: time.Time{}},
	}
	for _, tc := range cases {
		got := NormalizeEpoch(tc.value)
		if !got.Equal(tc.want) {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
}
