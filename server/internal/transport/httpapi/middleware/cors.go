package middleware

import (
	"slices"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/chenbb0128/weavepress/server/internal/config"
)

func CORS(cfg config.CORSConfig) gin.HandlerFunc {
	allowedMethods := upperValues(cfg.AllowedMethods)
	allowedHeaders := cfg.AllowedHeaders

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}

		if originAllowed(origin, cfg.AllowedOrigins) {
			if slices.Contains(cfg.AllowedOrigins, "*") && !cfg.AllowCredentials {
				c.Header("Access-Control-Allow-Origin", "*")
			} else {
				c.Header("Access-Control-Allow-Origin", origin)
			}
			c.Header("Vary", "Origin, Access-Control-Request-Method, Access-Control-Request-Headers")
			c.Header("Access-Control-Allow-Methods", strings.Join(allowedMethods, ", "))
			c.Header("Access-Control-Allow-Headers", strings.Join(allowedHeaders, ", "))
			if cfg.AllowCredentials {
				c.Header("Access-Control-Allow-Credentials", "true")
			}
		}

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

func originAllowed(origin string, allowed []string) bool {
	return slices.Contains(allowed, "*") || slices.Contains(allowed, origin)
}

func upperValues(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strings.ToUpper(value))
	}
	return out
}
