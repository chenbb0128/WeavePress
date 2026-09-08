package middleware

import (
	"log/slog"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"github.com/chenbb0128/weavepress/server/internal/platform/requestid"
	"github.com/chenbb0128/weavepress/server/internal/platform/telemetry"
	"github.com/chenbb0128/weavepress/server/internal/transport/httpapi/response"
)

func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				id, _ := requestid.FromContext(c.Request.Context())
				attrs := []any{"request_id", id, "panic", recovered, "stack", string(debug.Stack())}
				if traceID := telemetry.TraceIDFromContext(c.Request.Context()); traceID != "" {
					attrs = append(attrs, "trace_id", traceID)
				}
				logger.Error("panic recovered", attrs...)
				response.Error(c, response.Internal(nil))
			}
		}()

		c.Next()
	}
}
