package aiwriting

import "context"

type generationByJobStore interface {
	GetGenerationByJobID(context.Context, uint64) (Generation, error)
}

var _ generationByJobStore = (Store)(nil)
