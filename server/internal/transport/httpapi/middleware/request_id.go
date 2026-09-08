package middleware

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/chenbb0128/weavepress/server/internal/platform/requestid"
	"github.com/chenbb0128/weavepress/server/internal/transport/httpapi/response"
)

func RequestID(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(requestid.Header)
		if !requestid.IsValid(id) {
			generated, err := requestid.New()
			if err != nil {
				logger.Error("generate request id failed", "error", err)
				response.Error(c, response.Internal(err))
				return
			}
			id = generated
		}

		ctx := requestid.WithContext(c.Request.Context(), id)
		c.Request = c.Request.WithContext(ctx)
		c.Set(requestid.GinKey, id)
		c.Header(requestid.Header, id)

		c.Next()
	}
}
