package workers

import "github.com/hibiken/asynq"

import "github.com/chenbb0128/weavepress/server/internal/modules/content"

func NewMux(contentService *content.Service) *asynq.ServeMux {
	mux := asynq.NewServeMux()
	mux.HandleFunc(content.TaskCollectArticle, contentService.HandleTask)
	return mux
}
