package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCleanTextStripsElicitationsBlock(t *testing.T) {
	raw := "1+1 等于 2。\n\n<ElicitationsGroup message=\"继续？\">\n  <Elicitation label=\"a\" query=\"b\"/>\n</ElicitationsGroup>"
	assert.Equal(t, "1+1 等于 2。", cleanText(raw))
}

func TestCleanTextStripsSelfClosingElicitations(t *testing.T) {
	assert.Equal(t, "answer", cleanText("answer <ElicitationsGroup/>"))
}

func TestCleanTextHoldsBackPartialMarker(t *testing.T) {
	// A partial marker at the tail must never be emitted as literal text.
	assert.Equal(t, "answer", cleanText("answer <Elicitat"))
	assert.Equal(t, "answer", cleanText("answer <ElicitationsGroup"))
}

func TestCleanTextKeepsOrdinaryText(t *testing.T) {
	assert.Equal(t, "plain answer", cleanText("  plain answer  "))
}

func TestDeltaTrackerNeverEmitsArtifact(t *testing.T) {
	tracker := &deltaTracker{}
	assert.Equal(t, "answer ", tracker.push("answer "))
	// The partial marker is held back, so nothing new is emitted yet.
	assert.Equal(t, "", tracker.push("answer <Elicitat"))
	// Once the block is recognised the tail is dropped for good.
	assert.Equal(t, "done ", tracker.push("answer done <ElicitationsGroup/>"))
}

func TestCookieCacheKeyIgnoresRotatingTickets(t *testing.T) {
	a := "SAPISID=sapisid; __Secure-1PSID=psid; __Secure-1PSIDTS=one; SIDCC=one"
	b := "SAPISID=sapisid; __Secure-1PSID=psid; __Secure-1PSIDTS=two; SIDCC=two"
	assert.Equal(t, cookieCacheKey(a), cookieCacheKey(b))
	other := "SAPISID=other; __Secure-1PSID=psid; __Secure-1PSIDTS=one; SIDCC=one"
	assert.NotEqual(t, cookieCacheKey(a), cookieCacheKey(other))
}
