package workers

import (
	"github.com/hibiken/asynq"

	"github.com/chenbb0128/weavepress/server/internal/modules/aiwriting"
	"github.com/chenbb0128/weavepress/server/internal/modules/content"
	"github.com/chenbb0128/weavepress/server/internal/modules/editorial"
)

func NewMux(contentService *content.Service, editorialService *editorial.Service, aiService *aiwriting.Service) *asynq.ServeMux {
	mux := asynq.NewServeMux()
	if contentService != nil {
		mux.HandleFunc(content.TaskCollectArticle, contentService.HandleTask)
	}
	if editorialService != nil {
		mux.HandleFunc(editorial.TaskPublishWeChat, editorialService.HandleTask)
	}
	if aiService != nil {
		mux.HandleFunc(aiwriting.TaskAnalyze, aiService.HandleAnalyzeTask)
		mux.HandleFunc(aiwriting.TaskGenerate, aiService.HandleGenerateTask)
	}
	return mux
}
