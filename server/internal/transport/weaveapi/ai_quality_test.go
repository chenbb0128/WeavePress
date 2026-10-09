package weaveapi

import (
	"context"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chenbb0128/weavepress/server/internal/modules/aiwriting"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
)

func (s *fakeAIService) Quality(_ context.Context, articleID uint64, input aiwriting.QualityInput) (aiwriting.QualityReport, error) {
	s.call, s.articleID = "quality", articleID
	return aiwriting.QualityReport{Issues: []aiwriting.QualityIssue{{Code: "TITLE_DUPLICATE", Severity: "error", Message: "重新表达标题"}}}, s.err
}

type slowQualityService struct{ *fakeAIService }

func (s slowQualityService) Quality(ctx context.Context, articleID uint64, input aiwriting.QualityInput) (aiwriting.QualityReport, error) {
	select {
	case <-time.After(100 * time.Millisecond):
	case <-ctx.Done():
		return aiwriting.QualityReport{}, ctx.Err()
	}
	return s.fakeAIService.Quality(ctx, articleID, input)
}

func TestAIQualityRequestExtendsOnlyItsWriteDeadline(t *testing.T) {
	api, token := newAITestAPI(t, slowQualityService{&fakeAIService{}}, workspace.RoleEditor)
	engine := gin.New()
	api.Register(engine)
	server := httptest.NewUnstartedServer(engine)
	server.Config.WriteTimeout = 30 * time.Millisecond
	server.Start()
	defer server.Close()
	req, err := http.NewRequest(http.MethodPost, server.URL+"/api/articles/12/ai-quality", strings.NewReader(`{"title":"标题","blocks":[],"semantic":true}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	result, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Body.Close()
	body, err := io.ReadAll(result.Body)
	if err != nil || result.StatusCode != http.StatusOK || !strings.Contains(string(body), "TITLE_DUPLICATE") {
		t.Fatalf("status=%d body=%s err=%v", result.StatusCode, body, err)
	}
}

func (s *fakeAIService) Rewrite(_ context.Context, articleID uint64, input aiwriting.RewriteInput) (aiwriting.RewriteSuggestion, error) {
	s.call, s.articleID = "rewrite", articleID
	return aiwriting.RewriteSuggestion{TargetIndex: input.TargetIndex, Type: "title", Text: "新标题"}, s.err
}

func TestAIQualityRoutesRequireAuthentication(t *testing.T) {
	api, _ := newAITestAPI(t, &fakeAIService{}, workspace.RoleEditor)
	for _, path := range []string{"/api/articles/12/ai-quality", "/api/articles/12/ai-rewrite"} {
		recorder := performRequest(t, api, http.MethodPost, path, `{"title":"标题","blocks":[]}`, "")
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s status=%d", path, recorder.Code)
		}
	}
}

func TestAIQualityRoutesRejectMissingRewriteTarget(t *testing.T) {
	service := &fakeAIService{}
	api, token := newAITestAPI(t, service, workspace.RoleEditor)
	recorder := performRequest(t, api, http.MethodPost, "/api/articles/12/ai-rewrite", `{"title":"标题","blocks":[]}`, token)
	if recorder.Code != http.StatusUnprocessableEntity && recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body)
	}
	if service.call != "" {
		t.Fatalf("unexpected call=%s", service.call)
	}
}

func TestAIQualityRoutesReturnLocatedFindingsAndSuggestion(t *testing.T) {
	for _, path := range []string{"ai-quality", "ai-rewrite"} {
		service := &fakeAIService{}
		api, token := newAITestAPI(t, service, workspace.RoleEditor)
		body := `{"title":"标题","blocks":[],"targetIndex":-1}`
		if path == "ai-quality" {
			body = `{"title":"标题","blocks":[],"semantic":false}`
		}
		recorder := performRequest(t, api, http.MethodPost, "/api/articles/12/"+path, body, token)
		if recorder.Code != http.StatusOK || service.articleID != 12 {
			t.Fatalf("%s status=%d body=%s call=%s", path, recorder.Code, recorder.Body, service.call)
		}
		if path == "ai-quality" && !strings.Contains(recorder.Body.String(), "TITLE_DUPLICATE") {
			t.Fatal(recorder.Body.String())
		}
		if path == "ai-rewrite" && !strings.Contains(recorder.Body.String(), "新标题") {
			t.Fatal(recorder.Body.String())
		}
	}
}
