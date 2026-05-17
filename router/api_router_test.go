package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

const anonymousTokenUsageQueryPath = "/api/usage/token/query"

func setupAnonymousTokenUsageRouterTest(t *testing.T) *gin.Engine {
	t.Helper()

	gin.SetMode(gin.TestMode)

	oldServerAddress := system_setting.ServerAddress
	oldRedisEnabled := common.RedisEnabled
	oldGlobalAPIRateLimitEnable := common.GlobalApiRateLimitEnable
	oldCriticalRateLimitEnable := common.CriticalRateLimitEnable

	system_setting.ServerAddress = "https://console.example.com"
	common.RedisEnabled = false
	common.GlobalApiRateLimitEnable = false
	common.CriticalRateLimitEnable = false

	t.Cleanup(func() {
		system_setting.ServerAddress = oldServerAddress
		common.RedisEnabled = oldRedisEnabled
		common.GlobalApiRateLimitEnable = oldGlobalAPIRateLimitEnable
		common.CriticalRateLimitEnable = oldCriticalRateLimitEnable
	})

	router := gin.New()
	SetApiRouter(router)
	return router
}

func performAnonymousTokenUsageRequest(router *gin.Engine, method string, origin string) *httptest.ResponseRecorder {
	body := strings.NewReader(`{"api_key":""}`)
	req := httptest.NewRequest(method, anonymousTokenUsageQueryPath, body)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodOptions {
		req.Header.Set("Access-Control-Request-Method", http.MethodPost)
		req.Header.Set("Access-Control-Request-Headers", "content-type, cache-control, pragma")
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	return recorder
}

func TestAnonymousTokenUsageQueryNoStoreAndRestrictedCORS(t *testing.T) {
	router := setupAnonymousTokenUsageRouterTest(t)

	recorder := performAnonymousTokenUsageRequest(router, http.MethodPost, "https://console.example.com")

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if got := recorder.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("expected Cache-Control to include no-store, got %q", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://console.example.com" {
		t.Fatalf("expected restricted allow origin, got %q", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got == "*" {
		t.Fatalf("anonymous token usage query must not allow wildcard CORS")
	}
}

func TestAnonymousTokenUsageQueryRejectsUntrustedOrigin(t *testing.T) {
	router := setupAnonymousTokenUsageRouterTest(t)

	recorder := performAnonymousTokenUsageRequest(router, http.MethodPost, "https://evil.example.com")

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, recorder.Code)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected no CORS allow origin for rejected origin, got %q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("expected rejected response to include no-store, got %q", got)
	}
}

func TestAnonymousTokenUsageQueryAllowsSameOriginHost(t *testing.T) {
	router := setupAnonymousTokenUsageRouterTest(t)

	recorder := performAnonymousTokenUsageRequest(router, http.MethodPost, "http://example.com")

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "http://example.com" {
		t.Fatalf("expected same-origin allow origin, got %q", got)
	}
	if got := recorder.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("expected Cache-Control to include no-store, got %q", got)
	}
}

func TestAnonymousTokenUsageQueryPreflightUsesRestrictedCORS(t *testing.T) {
	router := setupAnonymousTokenUsageRouterTest(t)

	recorder := performAnonymousTokenUsageRequest(router, http.MethodOptions, "https://console.example.com")

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, recorder.Code)
	}
	if got := recorder.Header().Get("Cache-Control"); !strings.Contains(got, "no-store") {
		t.Fatalf("expected Cache-Control to include no-store, got %q", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://console.example.com" {
		t.Fatalf("expected restricted allow origin, got %q", got)
	}
	if got := recorder.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, http.MethodPost) {
		t.Fatalf("expected Access-Control-Allow-Methods to include POST, got %q", got)
	}
	if got := strings.ToLower(recorder.Header().Get("Access-Control-Allow-Headers")); !strings.Contains(got, "content-type") {
		t.Fatalf("expected Access-Control-Allow-Headers to include content-type, got %q", got)
	}
}
