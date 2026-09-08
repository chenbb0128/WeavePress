package middleware

import (
	"time"

	"github.com/gin-gonic/gin"

	platformmetrics "github.com/chenbb0128/weavepress/server/internal/platform/metrics"
)

func Metrics(metrics *platformmetrics.Metrics) gin.HandlerFunc {
	return func(c *gin.Context) {
		if metrics == nil {
			c.Next()
			return
		}

		metrics.IncHTTPInFlight()
		start := time.Now()
		defer metrics.DecHTTPInFlight()

		c.Next()

		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		metrics.ObserveHTTPRequest(
			c.Request.Method,
			route,
			c.Writer.Status(),
			time.Since(start),
			c.Writer.Size(),
		)
	}
}
