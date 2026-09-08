package workers

import "github.com/hibiken/asynq"

import (
	"github.com/chenbb0128/weavepress/server/internal/modules/content"
	"github.com/chenbb0128/weavepress/server/internal/modules/editorial"
)

func NewMux(contentService *content.Service, editorialService *editorial.Service) *asynq.ServeMux {
	mux := asynq.NewServeMux()
	mux.HandleFunc(content.TaskCollectArticle, contentService.HandleTask)
	mux.HandleFunc(editorial.TaskPublishWeChat, editorialService.HandleTask)
	return mux
}
