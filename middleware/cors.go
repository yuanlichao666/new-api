package middleware

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func CORS() gin.HandlerFunc {
	config := cors.DefaultConfig()
	config.AllowAllOrigins = true
	config.AllowCredentials = true
	config.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{"*"}
	return cors.New(config)
}

func AnonymousTokenUsageCORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin != "" {
			normalizedOrigin := normalizeCORSOrigin(origin)
			if normalizedOrigin == "" || !isAnonymousTokenUsageOriginAllowed(c.Request, normalizedOrigin) {
				clearCORSHeaders(c)
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			setAnonymousTokenUsageCORSHeaders(c, normalizedOrigin)
		}

		if c.Request.Method == http.MethodOptions {
			if !isAnonymousTokenUsagePreflightAllowed(c) {
				clearCORSHeaders(c)
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func normalizeCORSOrigin(origin string) string {
	parsed, err := url.Parse(strings.TrimSpace(origin))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}

	return scheme + "://" + strings.ToLower(parsed.Host)
}

func isAnonymousTokenUsageOriginAllowed(r *http.Request, origin string) bool {
	if origin == normalizeCORSOrigin(system_setting.ServerAddress) {
		return true
	}
	return origin == requestOrigin(r)
}

func requestOrigin(r *http.Request) string {
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return ""
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwardedProto := firstForwardedHeaderValue(r.Header.Get("X-Forwarded-Proto")); forwardedProto == "http" || forwardedProto == "https" {
		scheme = forwardedProto
	}

	return scheme + "://" + strings.ToLower(host)
}

func firstForwardedHeaderValue(value string) string {
	if value == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(strings.Split(value, ",")[0]))
}

func isAnonymousTokenUsagePreflightAllowed(c *gin.Context) bool {
	requestMethod := strings.TrimSpace(c.GetHeader("Access-Control-Request-Method"))
	if requestMethod != "" && !strings.EqualFold(requestMethod, http.MethodPost) {
		return false
	}

	for _, header := range strings.Split(c.GetHeader("Access-Control-Request-Headers"), ",") {
		header = strings.ToLower(strings.TrimSpace(header))
		if header == "" {
			continue
		}
		switch header {
		case "content-type", "cache-control", "pragma":
		default:
			return false
		}
	}
	return true
}

func setAnonymousTokenUsageCORSHeaders(c *gin.Context, origin string) {
	c.Header("Access-Control-Allow-Origin", origin)
	c.Header("Access-Control-Allow-Methods", "POST, OPTIONS")
	c.Header("Access-Control-Allow-Headers", "Content-Type, Cache-Control, Pragma")
	c.Header("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")
}

func clearCORSHeaders(c *gin.Context) {
	header := c.Writer.Header()
	header.Del("Access-Control-Allow-Origin")
	header.Del("Access-Control-Allow-Methods")
	header.Del("Access-Control-Allow-Headers")
}

func PoweredBy() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-New-Api-Version", common.Version)
		c.Next()
	}
}
