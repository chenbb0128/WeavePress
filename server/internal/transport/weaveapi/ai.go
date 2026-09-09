package weaveapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/chenbb0128/weavepress/server/internal/modules/aiwriting"
	"github.com/chenbb0128/weavepress/server/internal/transport/httpapi/request"
	"github.com/chenbb0128/weavepress/server/internal/transport/httpapi/response"
)

const (
	codeAIAnalysisCreate   = "ai:analysis:create"
	codeAIAnalysisView     = "ai:analysis:view"
	codeAIGenerationCreate = "ai:generation:create"
	codeAIGenerationView   = "ai:generation:view"
	codeAIJobRetry         = "ai:job:retry"
)

type AIService interface {
	Status() aiwriting.Status
	StartAnalysis(context.Context, uint64, uint64, bool) (aiwriting.Job, bool, error)
	Analyses(context.Context, uint64, int, int) (aiwriting.Page[aiwriting.Analysis], error)
	Analysis(context.Context, uint64) (aiwriting.Analysis, error)
	StartGeneration(context.Context, uint64, uint64, aiwriting.GenerationParams) (aiwriting.Generation, aiwriting.Job, bool, error)
	Generation(context.Context, uint64) (aiwriting.Generation, error)
	Jobs(context.Context, aiwriting.JobFilter, int, int) (aiwriting.Page[aiwriting.Job], error)
	Job(context.Context, uint64) (aiwriting.Job, error)
	Retry(context.Context, uint64, uint64) (aiwriting.Job, error)
}

type analysisInput struct {
	Force bool `json:"force"`
}

func (analysisInput) Validate() []response.ValidationDetail { return nil }

type generationInput struct {
	AngleID                string `json:"angleId"`
	Audience               string `json:"audience"`
	Tone                   string `json:"tone"`
	TargetWords            int    `json:"targetWords"`
	AdditionalInstructions string `json:"additionalInstructions"`
	IdempotencyKey         string `json:"idempotencyKey"`
}

func (i generationInput) Validate() []response.ValidationDetail {
	var details []response.ValidationDetail
	if strings.TrimSpace(i.AngleID) == "" {
		details = append(details, response.ValidationDetail{Field: "angleId", Reason: "required"})
	}
	audienceRunes := utf8.RuneCountInString(i.Audience)
	if strings.TrimSpace(i.Audience) == "" {
		details = append(details, response.ValidationDetail{Field: "audience", Reason: "required"})
	} else if audienceRunes > 100 {
		details = append(details, response.ValidationDetail{Field: "audience", Reason: "max_length"})
	}
	if _, ok := aiwriting.AllowedTones[i.Tone]; !ok {
		details = append(details, response.ValidationDetail{Field: "tone", Reason: "invalid"})
	}
	if i.TargetWords < 300 || i.TargetWords > 5000 {
		details = append(details, response.ValidationDetail{Field: "targetWords", Reason: "range"})
	}
	if utf8.RuneCountInString(i.AdditionalInstructions) > 500 {
		details = append(details, response.ValidationDetail{Field: "additionalInstructions", Reason: "max_length"})
	}
	if len(i.IdempotencyKey) < 8 || len(i.IdempotencyKey) > 128 {
		details = append(details, response.ValidationDetail{Field: "idempotencyKey", Reason: "length"})
	} else if !asciiOnly(i.IdempotencyKey) {
		details = append(details, response.ValidationDetail{Field: "idempotencyKey", Reason: "ascii"})
	}
	return details
}

func (i generationInput) params() aiwriting.GenerationParams {
	return aiwriting.GenerationParams{
		AngleID:                i.AngleID,
		Audience:               i.Audience,
		Tone:                   i.Tone,
		TargetWords:            i.TargetWords,
		AdditionalInstructions: i.AdditionalInstructions,
		IdempotencyKey:         i.IdempotencyKey,
	}
}

func (a *API) aiStatus(c *gin.Context) {
	response.OK(c, a.ai.Status())
}

func (a *API) startAIAnalysis(c *gin.Context) {
	articleID, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.Error(c, response.BadRequest("文章 ID 不正确", err))
		return
	}
	var input analysisInput
	if bindErr := request.BindJSON(c, &input); bindErr != nil {
		response.Error(c, bindErr)
		return
	}
	user, err := a.currentUser(c)
	if err != nil {
		a.writeError(c, err)
		return
	}
	job, reused, err := a.ai.StartAnalysis(c.Request.Context(), articleID, user.ID, input.Force)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.JSON(c, http.StatusAccepted, gin.H{"job": job, "reused": reused})
}

func (a *API) listAIAnalyses(c *gin.Context) {
	articleID, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.Error(c, response.BadRequest("文章 ID 不正确", err))
		return
	}
	page, pageSize, appErr := aiPagination(c)
	if appErr != nil {
		response.Error(c, appErr)
		return
	}
	result, err := a.ai.Analyses(c.Request.Context(), articleID, page, pageSize)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, result)
}

func (a *API) getAIAnalysis(c *gin.Context) {
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.Error(c, response.BadRequest("分析 ID 不正确", err))
		return
	}
	result, err := a.ai.Analysis(c.Request.Context(), id)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, result)
}

func (a *API) startAIGeneration(c *gin.Context) {
	analysisID, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.Error(c, response.BadRequest("分析 ID 不正确", err))
		return
	}
	var input generationInput
	if bindErr := request.BindJSON(c, &input); bindErr != nil {
		response.Error(c, bindErr)
		return
	}
	user, err := a.currentUser(c)
	if err != nil {
		a.writeError(c, err)
		return
	}
	generation, job, reused, err := a.ai.StartGeneration(c.Request.Context(), analysisID, user.ID, input.params())
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.JSON(c, http.StatusAccepted, gin.H{"generation": generation, "job": job, "reused": reused})
}

func (a *API) getAIGeneration(c *gin.Context) {
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.Error(c, response.BadRequest("生成结果 ID 不正确", err))
		return
	}
	result, err := a.ai.Generation(c.Request.Context(), id)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, result)
}

func (a *API) listAIJobs(c *gin.Context) {
	page, pageSize, appErr := aiPagination(c)
	if appErr != nil {
		response.Error(c, appErr)
		return
	}
	filter, appErr := aiJobFilter(c)
	if appErr != nil {
		response.Error(c, appErr)
		return
	}
	result, err := a.ai.Jobs(c.Request.Context(), filter, page, pageSize)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, result)
}

func (a *API) getAIJob(c *gin.Context) {
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.Error(c, response.BadRequest("AI 任务 ID 不正确", err))
		return
	}
	result, err := a.ai.Job(c.Request.Context(), id)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, result)
}

func (a *API) retryAIJob(c *gin.Context) {
	id, err := parsePositiveID(c.Param("id"))
	if err != nil {
		response.Error(c, response.BadRequest("AI 任务 ID 不正确", err))
		return
	}
	user, err := a.currentUser(c)
	if err != nil {
		a.writeError(c, err)
		return
	}
	result, err := a.ai.Retry(c.Request.Context(), id, user.ID)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.JSON(c, http.StatusAccepted, result)
}

func aiPagination(c *gin.Context) (int, int, *response.AppError) {
	page, pageSize := 1, 20
	var details []response.ValidationDetail
	if raw, exists := c.GetQuery("page"); exists {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			details = append(details, response.ValidationDetail{Field: "page", Reason: "positive_integer"})
		} else {
			page = parsed
		}
	}
	if raw, exists := c.GetQuery("pageSize"); exists {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			details = append(details, response.ValidationDetail{Field: "pageSize", Reason: "range"})
		} else {
			pageSize = parsed
		}
	}
	if len(details) > 0 {
		return 0, 0, response.ValidationFailed(details)
	}
	return page, pageSize, nil
}

func aiJobFilter(c *gin.Context) (aiwriting.JobFilter, *response.AppError) {
	filter := aiwriting.JobFilter{Type: c.Query("type"), Status: c.Query("status")}
	var details []response.ValidationDetail
	if filter.Type != "" && filter.Type != aiwriting.JobTypeAnalysis && filter.Type != aiwriting.JobTypeGeneration {
		details = append(details, response.ValidationDetail{Field: "type", Reason: "invalid"})
	}
	switch filter.Status {
	case "", aiwriting.JobQueued, aiwriting.JobRunning, aiwriting.JobCompleted, aiwriting.JobFailed:
	default:
		details = append(details, response.ValidationDetail{Field: "status", Reason: "invalid"})
	}
	if raw, exists := c.GetQuery("articleId"); exists {
		articleID, err := parsePositiveID(raw)
		if err != nil {
			details = append(details, response.ValidationDetail{Field: "articleId", Reason: "positive_integer"})
		} else {
			filter.ArticleID = articleID
		}
	}
	if len(details) > 0 {
		return aiwriting.JobFilter{}, response.ValidationFailed(details)
	}
	return filter, nil
}

func parsePositiveID(raw string) (uint64, error) {
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		return 0, strconv.ErrSyntax
	}
	return id, nil
}

func asciiOnly(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] > 127 {
			return false
		}
	}
	return true
}
