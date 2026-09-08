package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/chenbb0128/weavepress/server/internal/platform/requestid"
	"github.com/chenbb0128/weavepress/server/internal/platform/telemetry"
)

func AccessLog(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}

		id, _ := requestid.FromContext(c.Request.Context())
		attrs := []any{
			"request_id", id,
			"method", c.Request.Method,
			"route", route,
			"status", status,
			"latency", time.Since(start).String(),
			"client_ip", c.ClientIP(),
			"user_agent", c.Request.UserAgent(),
		}
		if traceID := telemetry.TraceIDFromContext(c.Request.Context()); traceID != "" {
			attrs = append(attrs, "trace_id", traceID)
		}
		if spanID := telemetry.SpanIDFromContext(c.Request.Context()); spanID != "" {
			attrs = append(attrs, "span_id", spanID)
		}
		if len(c.Errors) > 0 {
			attrs = append(attrs, "error", c.Errors.String())
		}

		switch {
		case status >= http.StatusInternalServerError:
			logger.Error("http request", attrs...)
		case status >= http.StatusBadRequest:
			logger.Warn("http request", attrs...)
		default:
			logger.Info("http request", attrs...)
		}
	}
}
