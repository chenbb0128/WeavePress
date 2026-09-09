package weaveapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/chenbb0128/weavepress/server/internal/collectors"
	"github.com/chenbb0128/weavepress/server/internal/config"
	"github.com/chenbb0128/weavepress/server/internal/modules/aiwriting"
	"github.com/chenbb0128/weavepress/server/internal/modules/authn"
	"github.com/chenbb0128/weavepress/server/internal/modules/content"
	"github.com/chenbb0128/weavepress/server/internal/modules/editorial"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
	"github.com/chenbb0128/weavepress/server/internal/transport/httpapi/request"
	"github.com/chenbb0128/weavepress/server/internal/transport/httpapi/response"
)

const claimsKey = "weavepress.claims"

type API struct {
	store     workspace.Store
	auth      *authn.Service
	content   *content.Service
	editorial *editorial.Service
	ai        AIService
	cfg       config.Config
}

func New(store workspace.Store, auth *authn.Service, contentService *content.Service, editorialService *editorial.Service, aiService AIService, cfg config.Config) *API {
	return &API{store: store, auth: auth, content: contentService, editorial: editorialService, ai: aiService, cfg: cfg}
}

func (a *API) Register(router *gin.Engine) {
	api := router.Group("/api")
	api.POST("/auth/login", a.login)
	api.POST("/auth/refresh", a.refresh)
	api.POST("/auth/logout", a.logout)

	protected := api.Group("")
	protected.Use(a.authenticate())
	protected.GET("/auth/codes", a.codes)
	protected.GET("/user/info", a.userInfo)
	protected.GET("/menu/all", a.menus)
	protected.GET("/dashboard", a.dashboard)

	protected.GET("/users", a.requireRole(workspace.RoleAdmin), a.listUsers)
	protected.POST("/users", a.requireRole(workspace.RoleAdmin), a.createUser)
	protected.PATCH("/users/:id", a.requireRole(workspace.RoleAdmin), a.updateUser)
	protected.PUT("/users/:id", a.requireRole(workspace.RoleAdmin), a.updateUser)
	protected.PUT("/users/:id/password", a.requireRole(workspace.RoleAdmin), a.resetPassword)

	protected.POST("/collection-jobs", a.submitCollection)
	protected.GET("/collection-jobs", a.listJobs)
	protected.GET("/collection-jobs/:id", a.getJob)
	protected.POST("/collection-jobs/:id/retry", a.retryJob)
	protected.GET("/articles", a.listArticles)
	protected.GET("/articles/:id", a.getArticle)
	protected.GET("/articles/:id/raw-url", a.rawURL)

	protected.POST("/drafts", a.createDraft)
	protected.GET("/drafts", a.listDrafts)
	protected.GET("/drafts/:id", a.getDraft)
	protected.PUT("/drafts/:id", a.updateDraft)
	protected.GET("/drafts/:id/versions", a.listDraftVersions)
	protected.POST("/drafts/:id/versions/:version/restore", a.restoreDraftVersion)
	protected.GET("/drafts/:id/preflight", a.draftPreflight)
	protected.POST("/drafts/:id/submit-review", a.submitDraftReview)
	protected.POST("/drafts/:id/review", a.requireRole(workspace.RoleAdmin), a.reviewDraft)
	protected.POST("/drafts/:id/publish", a.requireRole(workspace.RoleAdmin), a.publishDraft)
	protected.GET("/wechat/status", a.wechatStatus)
	protected.GET("/wechat-publish-jobs", a.listPublishJobs)
	protected.GET("/wechat-publish-jobs/:id", a.getPublishJob)
	protected.POST("/wechat-publish-jobs/:id/retry", a.requireRole(workspace.RoleAdmin), a.retryPublishJob)

	protected.GET("/ai/status", a.requireCode(codeAIAnalysisView), a.aiStatus)
	protected.POST("/articles/:id/ai-analyses", a.requireCode(codeAIAnalysisCreate), a.startAIAnalysis)
	protected.GET("/articles/:id/ai-analyses", a.requireCode(codeAIAnalysisView), a.listAIAnalyses)
	protected.GET("/ai-analyses/:id", a.requireCode(codeAIAnalysisView), a.getAIAnalysis)
	protected.POST("/ai-analyses/:id/generations", a.requireCode(codeAIGenerationCreate), a.startAIGeneration)
	protected.GET("/ai-generations/:id", a.requireCode(codeAIGenerationView), a.getAIGeneration)
	protected.GET("/ai-jobs", a.requireCode(codeAIAnalysisView), a.listAIJobs)
	protected.GET("/ai-jobs/:id", a.requireCode(codeAIAnalysisView), a.getAIJob)
	protected.POST("/ai-jobs/:id/retry", a.requireCode(codeAIJobRetry), a.retryAIJob)

	router.GET("/media/assets/:id", a.media("assets"))
	router.GET("/media/raw/:id", a.media("raw"))
}

type loginInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (i loginInput) Validate() []response.ValidationDetail {
	var details []response.ValidationDetail
	if len(strings.TrimSpace(i.Username)) < 3 {
		details = append(details, response.ValidationDetail{Field: "username", Reason: "min_length"})
	}
	if len(i.Password) < 8 {
		details = append(details, response.ValidationDetail{Field: "password", Reason: "min_length"})
	}
	return details
}

func (a *API) login(c *gin.Context) {
	var input loginInput
	if err := request.BindJSON(c, &input); err != nil {
		response.Error(c, err)
		return
	}
	session, err := a.auth.Login(c.Request.Context(), input.Username, input.Password, c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil {
		if errors.Is(err, authn.ErrRateLimited) {
			response.Error(c, response.RateLimited())
			return
		}
		response.Error(c, response.Unauthorized())
		return
	}
	a.setRefreshCookie(c, session.RefreshToken, session.RefreshUntil)
	response.OK(c, gin.H{"accessToken": session.AccessToken, "expiresIn": session.ExpiresIn})
}

func (a *API) refresh(c *gin.Context) {
	if !a.originAllowed(c) {
		response.Error(c, response.Forbidden())
		return
	}
	raw, _ := c.Cookie(a.cfg.Auth.RefreshCookie)
	session, err := a.auth.Refresh(c.Request.Context(), raw, c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil {
		a.clearRefreshCookie(c)
		response.Error(c, response.Unauthorized())
		return
	}
	a.setRefreshCookie(c, session.RefreshToken, session.RefreshUntil)
	response.OK(c, gin.H{"accessToken": session.AccessToken, "expiresIn": session.ExpiresIn})
}

func (a *API) logout(c *gin.Context) {
	if !a.originAllowed(c) {
		response.Error(c, response.Forbidden())
		return
	}
	raw, _ := c.Cookie(a.cfg.Auth.RefreshCookie)
	_ = a.auth.Logout(c.Request.Context(), raw)
	a.clearRefreshCookie(c)
	response.OK(c, true)
}

func (a *API) codes(c *gin.Context) {
	claims := mustClaims(c)
	response.OK(c, a.auth.Permissions(claims.Role))
}
func (a *API) userInfo(c *gin.Context) {
	user, err := a.currentUser(c)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, gin.H{"userId": strconv.FormatUint(user.ID, 10), "username": user.Username, "realName": user.Nickname, "avatar": user.Avatar, "roles": []string{string(user.Role)}, "homePath": "/dashboard", "desc": "WeavePress 内容团队"})
}
func (a *API) menus(c *gin.Context) { response.OK(c, []any{}) }

func (a *API) dashboard(c *gin.Context) {
	data, err := a.content.Dashboard(c.Request.Context())
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, data)
}

func (a *API) listUsers(c *gin.Context) {
	page, size := pagination(c)
	data, err := a.store.ListUsers(c.Request.Context(), strings.TrimSpace(c.Query("keyword")), c.Query("status"), page, size)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, data)
}

type createUserInput struct {
	Username string         `json:"username"`
	Password string         `json:"password"`
	RealName string         `json:"realName"`
	Role     workspace.Role `json:"role"`
}

func (i createUserInput) Validate() []response.ValidationDetail {
	var d []response.ValidationDetail
	if len(i.Username) < 3 || len(i.Username) > 32 {
		d = append(d, response.ValidationDetail{Field: "username", Reason: "length"})
	}
	if len(i.Password) < 8 {
		d = append(d, response.ValidationDetail{Field: "password", Reason: "min_length"})
	}
	if i.Role != workspace.RoleAdmin && i.Role != workspace.RoleEditor {
		d = append(d, response.ValidationDetail{Field: "role", Reason: "invalid"})
	}
	return d
}
func (a *API) createUser(c *gin.Context) {
	var input createUserInput
	if err := request.BindJSON(c, &input); err != nil {
		response.Error(c, err)
		return
	}
	user, err := a.auth.CreateUser(c.Request.Context(), input.Username, input.Password, input.RealName, input.Role)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.Created(c, fmt.Sprintf("/api/users/%d", user.ID), user)
}

type updateUserInput struct {
	RealName string         `json:"realName"`
	Role     workspace.Role `json:"role"`
	Status   string         `json:"status"`
}

func (i updateUserInput) Validate() []response.ValidationDetail {
	var d []response.ValidationDetail
	if strings.TrimSpace(i.RealName) == "" {
		d = append(d, response.ValidationDetail{Field: "realName", Reason: "required"})
	}
	if i.Role != workspace.RoleAdmin && i.Role != workspace.RoleEditor {
		d = append(d, response.ValidationDetail{Field: "role", Reason: "invalid"})
	}
	if i.Status != "active" && i.Status != "disabled" {
		d = append(d, response.ValidationDetail{Field: "status", Reason: "invalid"})
	}
	return d
}
func (a *API) updateUser(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, response.BadRequest("用户 ID 不正确", err))
		return
	}
	var input updateUserInput
	if bindErr := request.BindJSON(c, &input); bindErr != nil {
		response.Error(c, bindErr)
		return
	}
	current, _ := a.currentUser(c)
	if current.ID == id && input.Status == "disabled" {
		response.Error(c, response.Conflict("不能停用当前登录账号", nil))
		return
	}
	user, err := a.store.UpdateUser(c.Request.Context(), id, strings.TrimSpace(input.RealName), input.Role, input.Status)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, user)
}

type passwordInput struct {
	Password string `json:"password"`
}

func (i passwordInput) Validate() []response.ValidationDetail {
	if len(i.Password) < 8 {
		return []response.ValidationDetail{{Field: "password", Reason: "min_length"}}
	}
	return nil
}
func (a *API) resetPassword(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, response.BadRequest("用户 ID 不正确", err))
		return
	}
	var input passwordInput
	if bindErr := request.BindJSON(c, &input); bindErr != nil {
		response.Error(c, bindErr)
		return
	}
	if err := a.auth.ResetPassword(c.Request.Context(), id, input.Password); err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, true)
}

type submitInput struct {
	URL string `json:"url"`
}

func (i submitInput) Validate() []response.ValidationDetail {
	if strings.TrimSpace(i.URL) == "" || len(i.URL) > 4096 {
		return []response.ValidationDetail{{Field: "url", Reason: "invalid"}}
	}
	return nil
}
func (a *API) submitCollection(c *gin.Context) {
	var input submitInput
	if err := request.BindJSON(c, &input); err != nil {
		response.Error(c, err)
		return
	}
	user, _ := a.currentUser(c)
	result, err := a.content.Submit(c.Request.Context(), input.URL, user.ID)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.JSON(c, http.StatusAccepted, result)
}
func (a *API) listJobs(c *gin.Context) {
	page, size := pagination(c)
	data, err := a.content.ListJobs(c.Request.Context(), c.Query("status"), page, size)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, data)
}
func (a *API) getJob(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, response.BadRequest("任务 ID 不正确", err))
		return
	}
	data, err := a.content.Job(c.Request.Context(), id)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, data)
}
func (a *API) retryJob(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, response.BadRequest("任务 ID 不正确", err))
		return
	}
	data, err := a.content.Retry(c.Request.Context(), id)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, data)
}
func (a *API) listArticles(c *gin.Context) {
	page, size := pagination(c)
	data, err := a.content.ListArticles(c.Request.Context(), c.Query("keyword"), c.Query("sourceType"), c.Query("status"), page, size)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, data)
}
func (a *API) getArticle(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, response.BadRequest("文章 ID 不正确", err))
		return
	}
	data, err := a.content.Article(c.Request.Context(), id)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, data)
}
func (a *API) rawURL(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, response.BadRequest("文章 ID 不正确", err))
		return
	}
	data, err := a.content.Article(c.Request.Context(), id)
	if err != nil {
		a.writeError(c, err)
		return
	}
	if data.RawSnapshotURL == "" {
		response.Error(c, response.NotFound())
		return
	}
	response.OK(c, gin.H{"url": data.RawSnapshotURL})
}

type createDraftInput struct {
	ArticleID uint64 `json:"articleId"`
}

func (i createDraftInput) Validate() []response.ValidationDetail {
	if i.ArticleID == 0 {
		return []response.ValidationDetail{{Field: "articleId", Reason: "required"}}
	}
	return nil
}

func (a *API) createDraft(c *gin.Context) {
	var input createDraftInput
	if err := request.BindJSON(c, &input); err != nil {
		response.Error(c, err)
		return
	}
	user, _ := a.currentUser(c)
	draft, err := a.editorial.CreateFromArticle(c.Request.Context(), input.ArticleID, user.ID)
	if err != nil {
		a.writeError(c, err)
		return
	}
	a.decorateDraft(&draft)
	response.Created(c, fmt.Sprintf("/api/drafts/%d", draft.ID), draft)
}

func (a *API) listDrafts(c *gin.Context) {
	page, size := pagination(c)
	data, err := a.editorial.List(c.Request.Context(), c.Query("keyword"), c.Query("status"), page, size)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, data)
}

func (a *API) getDraft(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, response.BadRequest("稿件 ID 不正确", err))
		return
	}
	draft, err := a.editorial.Get(c.Request.Context(), id)
	if err != nil {
		a.writeError(c, err)
		return
	}
	a.decorateDraft(&draft)
	response.OK(c, draft)
}

type updateDraftInput struct {
	Title           string  `json:"title"`
	Author          string  `json:"author"`
	Digest          string  `json:"digest"`
	ContentHTML     string  `json:"contentHtml"`
	CoverAssetID    *uint64 `json:"coverAssetId"`
	ExpectedVersion uint    `json:"expectedVersion"`
	ChangeNote      string  `json:"changeNote"`
}

func (i updateDraftInput) Validate() []response.ValidationDetail {
	var details []response.ValidationDetail
	if strings.TrimSpace(i.Title) == "" {
		details = append(details, response.ValidationDetail{Field: "title", Reason: "required"})
	}
	if strings.TrimSpace(i.ContentHTML) == "" {
		details = append(details, response.ValidationDetail{Field: "contentHtml", Reason: "required"})
	}
	if i.ExpectedVersion == 0 {
		details = append(details, response.ValidationDetail{Field: "expectedVersion", Reason: "required"})
	}
	return details
}

func (a *API) updateDraft(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, response.BadRequest("稿件 ID 不正确", err))
		return
	}
	var input updateDraftInput
	if bindErr := request.BindJSON(c, &input); bindErr != nil {
		response.Error(c, bindErr)
		return
	}
	user, _ := a.currentUser(c)
	draft, err := a.editorial.Update(c.Request.Context(), id, user.ID, editorial.UpdateInput{
		Title: input.Title, Author: input.Author, Digest: input.Digest, ContentHTML: input.ContentHTML,
		CoverAssetID: input.CoverAssetID, ExpectedVersion: input.ExpectedVersion, ChangeNote: input.ChangeNote,
	})
	if err != nil {
		a.writeError(c, err)
		return
	}
	a.decorateDraft(&draft)
	response.OK(c, draft)
}

func (a *API) listDraftVersions(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, response.BadRequest("稿件 ID 不正确", err))
		return
	}
	versions, err := a.editorial.Versions(c.Request.Context(), id)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, versions)
}

type restoreDraftVersionInput struct {
	ExpectedVersion uint `json:"expectedVersion"`
}

func (i restoreDraftVersionInput) Validate() []response.ValidationDetail {
	if i.ExpectedVersion == 0 {
		return []response.ValidationDetail{{Field: "expectedVersion", Reason: "required"}}
	}
	return nil
}

func (a *API) restoreDraftVersion(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, response.BadRequest("稿件 ID 不正确", err))
		return
	}
	version64, err := strconv.ParseUint(c.Param("version"), 10, 32)
	if err != nil || version64 == 0 {
		response.Error(c, response.BadRequest("稿件版本不正确", err))
		return
	}
	var input restoreDraftVersionInput
	if bindErr := request.BindJSON(c, &input); bindErr != nil {
		response.Error(c, bindErr)
		return
	}
	user, _ := a.currentUser(c)
	draft, err := a.editorial.RestoreVersion(c.Request.Context(), id, user.ID, uint(version64), input.ExpectedVersion)
	if err != nil {
		a.writeError(c, err)
		return
	}
	a.decorateDraft(&draft)
	response.OK(c, draft)
}

func (a *API) draftPreflight(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, response.BadRequest("稿件 ID 不正确", err))
		return
	}
	result, err := a.editorial.Preflight(c.Request.Context(), id)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, result)
}

func (a *API) submitDraftReview(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, response.BadRequest("稿件 ID 不正确", err))
		return
	}
	user, _ := a.currentUser(c)
	draft, err := a.editorial.SubmitReview(c.Request.Context(), id, user.ID)
	if err != nil {
		a.writeError(c, err)
		return
	}
	a.decorateDraft(&draft)
	response.OK(c, draft)
}

type reviewDraftInput struct {
	Approved bool   `json:"approved"`
	Note     string `json:"note"`
}

func (a *API) reviewDraft(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, response.BadRequest("稿件 ID 不正确", err))
		return
	}
	var input reviewDraftInput
	if bindErr := request.BindJSON(c, &input); bindErr != nil {
		response.Error(c, bindErr)
		return
	}
	user, _ := a.currentUser(c)
	draft, err := a.editorial.Review(c.Request.Context(), id, user.ID, input.Approved, input.Note)
	if err != nil {
		a.writeError(c, err)
		return
	}
	a.decorateDraft(&draft)
	response.OK(c, draft)
}

func (a *API) publishDraft(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, response.BadRequest("稿件 ID 不正确", err))
		return
	}
	user, _ := a.currentUser(c)
	job, err := a.editorial.Publish(c.Request.Context(), id, user.ID)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.JSON(c, http.StatusAccepted, job)
}

func (a *API) wechatStatus(c *gin.Context) {
	response.OK(c, gin.H{
		"enabled":       a.editorial.PublishingEnabled(),
		"appConfigured": strings.TrimSpace(a.cfg.WeChat.AppID) != "" && strings.TrimSpace(a.cfg.WeChat.AppSecret) != "",
	})
}

func (a *API) listPublishJobs(c *gin.Context) {
	page, size := pagination(c)
	data, err := a.editorial.PublishJobs(c.Request.Context(), c.Query("status"), page, size)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, data)
}

func (a *API) getPublishJob(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, response.BadRequest("发布任务 ID 不正确", err))
		return
	}
	job, err := a.editorial.PublishJob(c.Request.Context(), id)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.OK(c, job)
}

func (a *API) retryPublishJob(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, response.BadRequest("发布任务 ID 不正确", err))
		return
	}
	user, _ := a.currentUser(c)
	job, err := a.editorial.RetryPublish(c.Request.Context(), id, user.ID)
	if err != nil {
		a.writeError(c, err)
		return
	}
	response.JSON(c, http.StatusAccepted, job)
}

func (a *API) decorateDraft(draft *editorial.Draft) {
	if draft.SourceArticle != nil {
		for index := range draft.SourceArticle.Assets {
			asset := &draft.SourceArticle.Assets[index]
			if asset.DownloadStatus == "completed" && asset.ObjectKey != "" {
				asset.MediaURL = a.content.SignMedia("assets", asset.ID)
			}
		}
	}
	draft.PreviewHTML = editorial.PreviewHTML(draft.ContentHTML, func(id uint64) string {
		return a.content.SignMedia("assets", id)
	})
}

func (a *API) media(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := parseID(c)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		expires, _ := strconv.ParseInt(c.Query("expires"), 10, 64)
		if !a.content.VerifyMedia(kind, id, expires, c.Query("signature")) {
			c.Status(http.StatusForbidden)
			return
		}
		key, mediaType, err := a.content.MediaObject(c.Request.Context(), kind, id)
		if err != nil || key == "" {
			c.Status(http.StatusNotFound)
			return
		}
		direct, err := a.content.ObjectStore().PrivateURL(key, time.Minute)
		if err != nil {
			c.Status(http.StatusBadGateway)
			return
		}
		if direct != "" {
			c.Redirect(http.StatusTemporaryRedirect, direct)
			return
		}
		reader, err := a.content.ObjectStore().Open(c.Request.Context(), key)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		defer reader.Close()
		data, err := io.ReadAll(reader)
		if err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		if kind == "raw" {
			c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=article-%d.html.gz", id))
		}
		c.Data(http.StatusOK, mediaType, data)
	}
}

func (a *API) authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			response.Error(c, response.Unauthorized())
			return
		}
		claims, err := a.auth.ParseAccessToken(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil {
			response.Error(c, response.Unauthorized())
			return
		}
		userID, err := strconv.ParseUint(claims.Subject, 10, 64)
		if err != nil {
			response.Error(c, response.Unauthorized())
			return
		}
		user, err := a.store.GetUserByID(c.Request.Context(), userID)
		if err != nil || user.Status != "active" || user.Role != claims.Role {
			response.Error(c, response.Unauthorized())
			return
		}
		c.Set(claimsKey, claims)
		c.Next()
	}
}
func (a *API) requireRole(role workspace.Role) gin.HandlerFunc {
	return func(c *gin.Context) {
		if mustClaims(c).Role != role {
			response.Error(c, response.Forbidden())
			return
		}
		c.Next()
	}
}
func (a *API) requireCode(code string) gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, permission := range a.auth.Permissions(mustClaims(c).Role) {
			if permission == code {
				c.Next()
				return
			}
		}
		response.Error(c, response.Forbidden())
	}
}
func mustClaims(c *gin.Context) authn.Claims {
	value, _ := c.Get(claimsKey)
	claims, _ := value.(authn.Claims)
	return claims
}
func (a *API) currentUser(c *gin.Context) (workspace.User, error) {
	id, err := strconv.ParseUint(mustClaims(c).Subject, 10, 64)
	if err != nil {
		return workspace.User{}, err
	}
	return a.store.GetUserByID(c.Request.Context(), id)
}
func (a *API) setRefreshCookie(c *gin.Context, value string, expires time.Time) {
	http.SetCookie(c.Writer, &http.Cookie{Name: a.cfg.Auth.RefreshCookie, Value: value, Path: "/api/auth", HttpOnly: true, Secure: a.cfg.Auth.CookieSecure, SameSite: http.SameSiteLaxMode, Expires: expires, MaxAge: int(time.Until(expires).Seconds())})
}
func (a *API) clearRefreshCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{Name: a.cfg.Auth.RefreshCookie, Value: "", Path: "/api/auth", HttpOnly: true, Secure: a.cfg.Auth.CookieSecure, SameSite: http.SameSiteLaxMode, Expires: time.Unix(0, 0), MaxAge: -1})
}
func (a *API) originAllowed(c *gin.Context) bool {
	raw := c.GetHeader("Origin")
	if raw == "" {
		return true
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	requestHost := c.Request.Host
	if parsed.Host == requestHost {
		return true
	}
	for _, allowed := range a.cfg.HTTP.CORS.AllowedOrigins {
		if strings.EqualFold(allowed, raw) {
			return true
		}
	}
	return false
}
func pagination(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}
func parseID(c *gin.Context) (uint64, error) { return strconv.ParseUint(c.Param("id"), 10, 64) }
func (a *API) writeError(c *gin.Context, err error) {
	var collectErr *collectors.Error
	var preflightErr *editorial.PreflightError
	switch {
	case errors.Is(err, workspace.ErrNotFound):
		response.Error(c, response.NotFound())
	case errors.Is(err, aiwriting.ErrNotConfigured):
		response.Error(c, response.DependencyUnavailable(err))
	case errors.Is(err, aiwriting.ErrInputTooLarge):
		response.Error(c, response.PayloadTooLarge(err))
	case errors.Is(err, aiwriting.ErrInvalidParameters),
		errors.Is(err, aiwriting.ErrOutputInvalid),
		errors.Is(err, aiwriting.ErrSourceReferenceInvalid),
		errors.Is(err, aiwriting.ErrQuoteMismatch),
		errors.Is(err, aiwriting.ErrAssetInvalid),
		errors.Is(err, aiwriting.ErrExcessiveSourceOverlap):
		response.Error(c, response.BadRequest("AI 请求内容不合法", err))
	case errors.Is(err, aiwriting.ErrArticleNotReady),
		errors.Is(err, aiwriting.ErrAnalysisNotReady),
		errors.Is(err, aiwriting.ErrJobNotRetryable),
		errors.Is(err, workspace.ErrJobStateConflict):
		response.Error(c, response.Conflict("AI 任务当前状态不允许此操作", err))
	case errors.Is(err, workspace.ErrUsernameTaken):
		response.Error(c, response.Conflict("用户名已存在", err))
	case errors.Is(err, workspace.ErrJobNotRetryable):
		response.Error(c, response.Conflict("任务当前不能重试", err))
	case errors.Is(err, editorial.ErrDraftNotEditable):
		response.Error(c, response.Conflict("稿件当前不能编辑", err))
	case errors.Is(err, editorial.ErrDraftVersionConflict):
		response.Error(c, response.Conflict("稿件已被其他人更新，请刷新后重试", err))
	case errors.Is(err, editorial.ErrDraftStateConflict):
		response.Error(c, response.Conflict("稿件状态不允许当前操作", err))
	case errors.Is(err, editorial.ErrPublishNotRetryable):
		response.Error(c, response.Conflict("发布任务当前不能重试", err))
	case errors.Is(err, editorial.ErrPublisherDisabled):
		response.Error(c, response.DependencyUnavailable(err))
	case errors.Is(err, editorial.ErrCoverRequired):
		response.Error(c, response.BadRequest("发布到微信公众号前必须选择封面", err))
	case errors.As(err, &preflightErr):
		message := "微信公众号发布前检查未通过"
		if len(preflightErr.Result.Issues) > 0 {
			message += "：" + preflightErr.Result.Issues[0].Message
		}
		response.Error(c, response.BadRequest(message, err))
	case errors.As(err, &collectErr):
		response.Error(c, response.BadRequest(collectErr.Message, err))
	default:
		response.Error(c, response.Internal(err))
	}
}
