package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

func Cache() func(c *gin.Context) {
	return func(c *gin.Context) {
		if c.Request.RequestURI == "/" || isCacheRevalidationRequest(c) {
			// A normal reload must revalidate the embedded index and async
			// chunks instead of reusing a week-old deployment for the tab.
			c.Header("Cache-Control", "no-cache")
		} else {
			c.Header("Cache-Control", "max-age=604800") // one week
		}
		c.Header("Cache-Version", "b688f2fb5be447c25e5aa3bd063087a83db32a288bf6a4f35f2d8db310e40b14")
		c.Next()
	}
}

// isCacheRevalidationRequest reports whether the client asked for a fresh copy
// instead of a cached one. A browser reload sends "max-age=0" while a hard
// refresh sends "no-cache", so both must revalidate.
func isCacheRevalidationRequest(c *gin.Context) bool {
	if strings.EqualFold(c.GetHeader("Pragma"), "no-cache") {
		return true
	}
	for directive := range strings.SplitSeq(c.GetHeader("Cache-Control"), ",") {
		directive = strings.TrimSpace(directive)
		if strings.EqualFold(directive, "no-cache") || strings.EqualFold(directive, "max-age=0") {
			return true
		}
	}
	return false
}
