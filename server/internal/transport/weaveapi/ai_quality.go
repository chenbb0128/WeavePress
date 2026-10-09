package weaveapi

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/chenbb0128/weavepress/server/internal/modules/aiwriting"
	"github.com/chenbb0128/weavepress/server/internal/transport/httpapi/request"
	"github.com/chenbb0128/weavepress/server/internal/transport/httpapi/response"
	"github.com/gin-gonic/gin"
)

type qualityRequest struct{ aiwriting.QualityInput }

func (i qualityRequest) Validate() []response.ValidationDetail {
	if strings.TrimSpace(i.Title) == "" || utf8.RuneCountInString(i.Title) > 255 {
		return []response.ValidationDetail{{Field: "title", Reason: "invalid"}}
	}
	if i.Blocks == nil || len(i.Blocks) > 5000 {
		return []response.ValidationDetail{{Field: "blocks", Reason: "invalid"}}
	}
	return nil
}

type rewriteRequest struct {
	aiwriting.QualityInput
	TargetIndex *int `json:"targetIndex"`
}

func (i rewriteRequest) Validate() []response.ValidationDetail {
	if details := (qualityRequest{i.QualityInput}).Validate(); len(details) > 0 {
		return details
	}
	if i.TargetIndex == nil || *i.TargetIndex < -1 || *i.TargetIndex >= len(i.Blocks) {
		return []response.ValidationDetail{{Field: "targetIndex", Reason: "invalid"}}
	}
	return nil
}

func (a *API) checkAIQuality(c *gin.Context) {
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.Error(c, response.BadRequest("文章 ID 不正确", err))
		return
	}
	var input qualityRequest
	if err := request.BindJSON(c, &input); err != nil {
		response.Error(c, err)
		return
	}
	if input.Semantic {
		extendQualityDeadline(c)
	}
	report, err := a.ai.Quality(c.Request.Context(), id, input.QualityInput)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, report)
}

func (a *API) rewriteAIBlock(c *gin.Context) {
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.Error(c, response.BadRequest("文章 ID 不正确", err))
		return
	}
	var input rewriteRequest
	if err := request.BindJSON(c, &input); err != nil {
		response.Error(c, err)
		return
	}
	extendQualityDeadline(c)
	suggestion, err := a.ai.Rewrite(c.Request.Context(), id, aiwriting.RewriteInput{QualityInput: input.QualityInput, TargetIndex: *input.TargetIndex})
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, suggestion)
}

func extendQualityDeadline(c *gin.Context) {
	// Gin unwraps the real writer. Recorder-only unit tests have no deadline;
	// production HTTP connections get a bounded deadline for this AI request.
	_ = http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(aiwriting.MaxQualityRequestTimeout + 10*time.Second))
}
