package weaveapi

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/chenbb0128/weavepress/server/internal/modules/aisettings"
	"github.com/chenbb0128/weavepress/server/internal/transport/httpapi/request"
	"github.com/chenbb0128/weavepress/server/internal/transport/httpapi/response"
)

const (
	codeAISettingsView   = "ai:settings:view"
	codeAISettingsUpdate = "ai:settings:update"
)

type AISettingsService interface {
	View(context.Context) (aisettings.SettingsView, error)
	Update(context.Context, uint64, aisettings.UpdateInput) (aisettings.SettingsView, error)
	TestConnection(context.Context, aisettings.TestInput) (aisettings.TestResult, error)
}

type aiSettingsInput struct {
	Enabled        bool   `json:"enabled"`
	ActiveProvider string `json:"activeProvider"`
	BaseURL        string `json:"baseUrl"`
	Model          string `json:"model"`
	APIKey         string `json:"apiKey"`
}

func (i aiSettingsInput) Validate() []response.ValidationDetail {
	var details []response.ValidationDetail
	if strings.TrimSpace(i.ActiveProvider) == "" {
		details = append(details, response.ValidationDetail{Field: "activeProvider", Reason: "required"})
	}
	if len(i.BaseURL) > 500 {
		details = append(details, response.ValidationDetail{Field: "baseUrl", Reason: "max_length"})
	}
	if strings.TrimSpace(i.Model) == "" {
		details = append(details, response.ValidationDetail{Field: "model", Reason: "required"})
	} else if len(i.Model) > 100 {
		details = append(details, response.ValidationDetail{Field: "model", Reason: "max_length"})
	}
	if len(i.APIKey) > 2048 {
		details = append(details, response.ValidationDetail{Field: "apiKey", Reason: "max_length"})
	}
	return details
}

func (i aiSettingsInput) updateInput() aisettings.UpdateInput {
	return aisettings.UpdateInput{
		Enabled: i.Enabled, ActiveProvider: i.ActiveProvider,
		BaseURL: i.BaseURL, Model: i.Model, APIKey: i.APIKey,
	}
}

type aiSettingsTestInput struct {
	ActiveProvider string `json:"activeProvider"`
	BaseURL        string `json:"baseUrl"`
	Model          string `json:"model"`
	APIKey         string `json:"apiKey"`
}

func (i aiSettingsTestInput) Validate() []response.ValidationDetail {
	return aiSettingsInput{
		ActiveProvider: i.ActiveProvider,
		BaseURL:        i.BaseURL,
		Model:          i.Model,
		APIKey:         i.APIKey,
	}.Validate()
}

func (i aiSettingsTestInput) testInput() aisettings.TestInput {
	return aisettings.TestInput{
		ActiveProvider: i.ActiveProvider,
		BaseURL:        i.BaseURL,
		Model:          i.Model,
		APIKey:         i.APIKey,
	}
}

func (a *API) getAISettings(c *gin.Context) {
	if a.aiSettings == nil {
		response.Error(c, response.Internal(aisettings.ErrNotConfigured))
		return
	}
	result, err := a.aiSettings.View(c.Request.Context())
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, result)
}

func (a *API) updateAISettings(c *gin.Context) {
	var input aiSettingsInput
	if bindErr := request.BindJSON(c, &input); bindErr != nil {
		response.Error(c, bindErr)
		return
	}
	user, err := a.currentUser(c)
	if err != nil {
		a.writeError(c, err)
		return
	}
	if a.aiSettings == nil {
		response.Error(c, response.Internal(aisettings.ErrNotConfigured))
		return
	}
	result, err := a.aiSettings.Update(c.Request.Context(), user.ID, input.updateInput())
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, result)
}

func (a *API) testAISettings(c *gin.Context) {
	var input aiSettingsTestInput
	if bindErr := request.BindJSON(c, &input); bindErr != nil {
		response.Error(c, bindErr)
		return
	}
	if a.aiSettings == nil {
		response.Error(c, response.Internal(aisettings.ErrNotConfigured))
		return
	}
	result, err := a.aiSettings.TestConnection(c.Request.Context(), input.testInput())
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, result)
}
