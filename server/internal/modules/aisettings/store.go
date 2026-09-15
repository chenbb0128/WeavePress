package aisettings

import "context"

type Store interface {
	GetRuntime(context.Context) (StoredRuntime, error)
	ListProviders(context.Context) ([]StoredProvider, error)
	GetProvider(context.Context, string) (StoredProvider, error)
	Update(context.Context, StoreUpdate) error
}
