package aisettings

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeStore struct {
	runtime   StoredRuntime
	providers map[string]StoredProvider
	update    StoreUpdate
}

func (s *fakeStore) GetRuntime(context.Context) (StoredRuntime, error) {
	return s.runtime, nil
}

func (s *fakeStore) ListProviders(context.Context) ([]StoredProvider, error) {
	result := make([]StoredProvider, 0, len(s.providers))
	for _, provider := range s.providers {
		provider.APICiphertext = bytes.Clone(provider.APICiphertext)
		result = append(result, provider)
	}
	return result, nil
}

func (s *fakeStore) GetProvider(_ context.Context, provider string) (StoredProvider, error) {
	value, ok := s.providers[provider]
	if !ok {
		return StoredProvider{}, ErrProviderNotConfigured
	}
	value.APICiphertext = bytes.Clone(value.APICiphertext)
	return value, nil
}

func (s *fakeStore) Update(_ context.Context, input StoreUpdate) error {
	s.update = input
	provider := s.providers[input.Provider]
	provider.Provider = input.Provider
	provider.BaseURL = input.BaseURL
	provider.Model = input.Model
	if !input.PreserveKey {
		provider.APICiphertext = bytes.Clone(input.APICiphertext)
	}
	s.providers[input.Provider] = provider
	s.runtime = StoredRuntime{Enabled: input.Enabled, ActiveProvider: input.ActiveProvider}
	return nil
}

func newTestService(t *testing.T, store *fakeStore) *Service {
	t.Helper()
	service, err := New(store, strings.Repeat("m", 32), func(context.Context, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestViewReturnsCatalogWithoutAPIKey(t *testing.T) {
	cipher, err := NewCipher(strings.Repeat("m", 32))
	if err != nil {
		t.Fatal(err)
	}
	secret, err := cipher.Encrypt([]byte("api-key-sensitive"))
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{
		runtime: StoredRuntime{Enabled: true, ActiveProvider: ProviderZhipu},
		providers: map[string]StoredProvider{
			ProviderZhipu: {Provider: ProviderZhipu, BaseURL: zhipuBaseURL, Model: "glm-custom", APICiphertext: secret},
		},
	}
	view, err := newTestService(t, store).View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Providers) != 3 {
		t.Fatalf("providers = %d, want 3", len(view.Providers))
	}
	if !view.Providers[0].KeyConfigured || view.Providers[0].Model != "glm-custom" {
		t.Fatalf("zhipu view = %#v", view.Providers[0])
	}
	if strings.Contains(strings.ToLower(strings.Join([]string{view.Providers[0].BaseURL, view.Providers[0].Model}, " ")), "api-key-sensitive") {
		t.Fatal("view leaked key")
	}
}

func TestUpdateEmptyKeyPreservesConfiguredKey(t *testing.T) {
	cipher, err := NewCipher(strings.Repeat("m", 32))
	if err != nil {
		t.Fatal(err)
	}
	existing, err := cipher.Encrypt([]byte("existing-key"))
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{
		runtime: StoredRuntime{Enabled: true, ActiveProvider: ProviderZhipu},
		providers: map[string]StoredProvider{
			ProviderZhipu: {Provider: ProviderZhipu, BaseURL: zhipuBaseURL, Model: "glm-5.3-flash", APICiphertext: existing},
		},
	}
	_, err = newTestService(t, store).Update(context.Background(), 7, UpdateInput{
		Enabled: true, ActiveProvider: ProviderZhipu, Model: "glm-5.3-flash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !store.update.PreserveKey || store.update.APICiphertext != nil {
		t.Fatalf("update = %#v", store.update)
	}
}

func TestUpdateKeepsProviderSettingsSeparate(t *testing.T) {
	store := &fakeStore{providers: map[string]StoredProvider{}}
	service := newTestService(t, store)
	_, err := service.Update(context.Background(), 7, UpdateInput{
		ActiveProvider: ProviderZhipu, Model: "glm-5.3-flash", APIKey: "zhipu-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Update(context.Background(), 7, UpdateInput{
		ActiveProvider: ProviderOpenAI, Model: "gpt-5-mini", APIKey: "openai-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(store.providers[ProviderZhipu].APICiphertext, store.providers[ProviderOpenAI].APICiphertext) {
		t.Fatal("provider keys were not stored separately")
	}
}

func TestUpdateRequiresKeyBeforeEnabling(t *testing.T) {
	store := &fakeStore{providers: map[string]StoredProvider{}}
	_, err := newTestService(t, store).Update(context.Background(), 7, UpdateInput{
		Enabled: true, ActiveProvider: ProviderOpenAI, Model: "gpt-5-mini",
	})
	if !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("Update() error = %v, want %v", err, ErrInvalidSettings)
	}
}

func TestForJobUsesRecordedModelAndLatestKey(t *testing.T) {
	cipher, err := NewCipher(strings.Repeat("m", 32))
	if err != nil {
		t.Fatal(err)
	}
	secret, err := cipher.Encrypt([]byte("latest-key"))
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{
		runtime: StoredRuntime{Enabled: true, ActiveProvider: ProviderZhipu},
		providers: map[string]StoredProvider{
			ProviderOpenAI: {Provider: ProviderOpenAI, BaseURL: openAIBaseURL, Model: "current-default", APICiphertext: secret},
		},
	}
	runtime, err := newTestService(t, store).ForJob(context.Background(), ProviderOpenAI, "recorded-model")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.Model != "recorded-model" || runtime.APIKey != "latest-key" || runtime.Provider != ProviderOpenAI {
		t.Fatalf("runtime = %#v", runtime)
	}
}

func TestActiveReturnsNotConfiguredWhenDisabled(t *testing.T) {
	store := &fakeStore{runtime: StoredRuntime{Enabled: false, ActiveProvider: ProviderZhipu}, providers: map[string]StoredProvider{}}
	_, err := newTestService(t, store).Active(context.Background())
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Active() error = %v, want %v", err, ErrNotConfigured)
	}
}
