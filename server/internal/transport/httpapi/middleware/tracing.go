package middleware

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/chenbb0128/weavepress/server/internal/platform/requestid"
)

func Tracing(serviceName string) gin.HandlerFunc {
	if serviceName == "" {
		serviceName = "weavepress"
	}
	tracer := otel.Tracer(serviceName + "/http")

	return func(c *gin.Context) {
		ctx := otel.GetTextMapPropagator().Extract(c.Request.Context(), propagation.HeaderCarrier(c.Request.Header))
		spanName := c.Request.Method + " " + c.Request.URL.Path
		ctx, span := tracer.Start(ctx, spanName,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("http.method", c.Request.Method),
				attribute.String("url.path", c.Request.URL.Path),
				attribute.String("client.address", c.ClientIP()),
			),
		)
		defer span.End()

		if id, ok := requestid.FromContext(ctx); ok {
			span.SetAttributes(attribute.String("request.id", id))
		}

		c.Request = c.Request.WithContext(ctx)
		c.Next()

		status := c.Writer.Status()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}

		span.SetName(c.Request.Method + " " + route)
		span.SetAttributes(
			attribute.String("http.route", route),
			attribute.Int("http.status_code", status),
			attribute.Int("http.response.body.size", c.Writer.Size()),
		)

		if len(c.Errors) > 0 {
			err := errors.New(c.Errors.String())
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return
		}
		if status >= http.StatusInternalServerError {
			span.SetStatus(codes.Error, http.StatusText(status))
		}
	}
}
