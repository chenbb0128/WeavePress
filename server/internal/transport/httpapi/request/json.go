package request

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/chenbb0128/weavepress/server/internal/transport/httpapi/response"
)

type Validator interface {
	Validate() []response.ValidationDetail
}

func BindJSON(c *gin.Context, dst any) *response.AppError {
	if !acceptsJSON(c.GetHeader("Accept")) {
		return response.NotAcceptable()
	}
	if !isJSONContentType(c.GetHeader("Content-Type")) {
		return response.UnsupportedMediaType()
	}

	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()

	var raw json.RawMessage
	if err := decoder.Decode(&raw); err != nil {
		return decodeError(err)
	}

	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return response.BadRequest("请求体必须是 JSON 对象", nil)
	}

	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return decodeError(err)
		}
		return response.BadRequest("请求体只能包含一个 JSON 值", nil)
	}

	strictDecoder := json.NewDecoder(bytes.NewReader(trimmed))
	strictDecoder.DisallowUnknownFields()
	if err := strictDecoder.Decode(dst); err != nil {
		return decodeObjectError(err)
	}

	if validator, ok := dst.(Validator); ok {
		if details := validator.Validate(); len(details) > 0 {
			return response.ValidationFailed(details)
		}
	}

	return nil
}

func isJSONContentType(header string) bool {
	if strings.TrimSpace(header) == "" {
		return false
	}

	mediaType, _, err := mime.ParseMediaType(header)
	if err != nil {
		return false
	}
	return mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
}

func acceptsJSON(header string) bool {
	if strings.TrimSpace(header) == "" {
		return true
	}

	for _, part := range strings.Split(header, ",") {
		mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		if params["q"] == "0" || params["q"] == "0.0" {
			continue
		}
		if mediaType == "*/*" || mediaType == "application/*" || mediaType == "application/json" || strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "/*+json") {
			return true
		}
	}

	return false
}

func decodeError(err error) *response.AppError {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return response.PayloadTooLarge(err)
	}
	if errors.Is(err, io.EOF) {
		return response.BadRequest("请求体不能为空", err)
	}

	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return response.BadRequest("请求体不是合法 JSON", err)
	}

	return response.BadRequest("请求体不是合法 JSON", err)
}

func decodeObjectError(err error) *response.AppError {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		field := typeErr.Field
		if field == "" {
			field = typeErr.Struct
		}
		return response.ValidationFailed([]response.ValidationDetail{{
			Field:  field,
			Reason: "invalid_type",
		}})
	}

	const unknownFieldPrefix = "json: unknown field "
	if strings.HasPrefix(err.Error(), unknownFieldPrefix) {
		field := strings.Trim(err.Error()[len(unknownFieldPrefix):], `"`)
		return response.ValidationFailed([]response.ValidationDetail{{
			Field:  field,
			Reason: "unknown",
		}})
	}

	return response.BadRequest("请求体不是合法 JSON", err)
}
