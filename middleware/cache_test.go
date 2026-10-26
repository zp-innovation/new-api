package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestCacheHonorsBrowserReloadRevalidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(Cache())
	router.GET("/static/js/app.js", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	normal := httptest.NewRecorder()
	router.ServeHTTP(normal, httptest.NewRequest(http.MethodGet, "/static/js/app.js", nil))
	assert.Equal(t, "max-age=604800", normal.Header().Get("Cache-Control"))

	reload := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/static/js/app.js", nil)
	request.Header.Set("Cache-Control", "no-cache")
	router.ServeHTTP(reload, request)
	assert.Equal(t, "no-cache", reload.Header().Get("Cache-Control"))

	// A plain reload sends max-age=0 with the other directives attached.
	maxAgeZero := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/static/js/app.js", nil)
	request.Header.Set("Cache-Control", "max-age=0")
	router.ServeHTTP(maxAgeZero, request)
	assert.Equal(t, "no-cache", maxAgeZero.Header().Get("Cache-Control"))

	combined := httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/static/js/app.js", nil)
	request.Header.Set("Cache-Control", "max-age=0, no-cache")
	router.ServeHTTP(combined, request)
	assert.Equal(t, "no-cache", combined.Header().Get("Cache-Control"))
}
