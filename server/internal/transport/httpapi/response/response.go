package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type successEnvelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

type errorEnvelope struct {
	Code    int                `json:"code"`
	Message string             `json:"message"`
	Details []ValidationDetail `json:"details,omitempty"`
}

func OK(c *gin.Context, data any) {
	JSON(c, http.StatusOK, data)
}

func Created(c *gin.Context, location string, data any) {
	if location != "" {
		c.Header("Location", location)
	}
	JSON(c, http.StatusCreated, data)
}

func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

func JSON(c *gin.Context, status int, data any) {
	if status == http.StatusNoContent {
		NoContent(c)
		return
	}
	c.Header("Content-Type", "application/json; charset=utf-8")
	c.JSON(status, successEnvelope{
		Code:    CodeSuccess,
		Message: "success",
		Data:    data,
	})
}

func Error(c *gin.Context, err error) {
	appErr := AsAppError(err)
	if appErr.HTTPStatus == http.StatusNoContent {
		NoContent(c)
		return
	}

	body := errorEnvelope{
		Code:    appErr.Code,
		Message: appErr.Message,
	}
	if len(appErr.Details) > 0 {
		body.Details = appErr.Details
	}

	c.Header("Content-Type", "application/json; charset=utf-8")
	c.AbortWithStatusJSON(appErr.HTTPStatus, body)
}
