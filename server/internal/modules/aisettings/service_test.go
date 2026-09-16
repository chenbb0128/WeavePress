package aisettings

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeConnectionTester struct {
	config RuntimeConfig
	calls  int
	err    error
}

func (t *fakeConnectionTester) Test(_ context.Context, config RuntimeConfig) error {
	t.calls++
	t.config = config
	return t.err
}

type fakeStore struct {
	runtime      StoredRuntime
	providers    map[string]StoredProvider
	update       StoreUpdate
	beforeUpdate func(*fakeStore)
}

type unsafeValidationError struct{ message string }

func (e *unsafeValidationError) Error() string   { return e.message }
func (e *unsafeValidationError) UnsafeURL() bool { return true }

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
	if s.beforeUpdate != nil {
		beforeUpdate := s.beforeUpdate
		s.beforeUpdate = nil
		beforeUpdate(s)
	}
	if input.CompareCredential {
		current := s.providers[input.Provider]
		if current.BaseURL != input.ExpectedBaseURL || !bytes.Equal(current.APICiphertext, input.ExpectedAPICiphertext) {
			return ErrSettingsConflict
		}
	}
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
	secret, err := cipher.Encrypt(ProviderZhipu, zhipuBaseURL, []byte("api-key-sensitive"))
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
	existing, err := cipher.Encrypt(ProviderZhipu, zhipuBaseURL, []byte("existing-key"))
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

func TestUpdateReencryptsPreservedKeyWhenBaseURLChanges(t *testing.T) {
	const oldBaseURL = "https://old.example/v1"
	const newBaseURL = "https://new.example/v1"
	cipher, err := NewCipher(strings.Repeat("m", 32))
	if err != nil {
		t.Fatal(err)
	}
	existing, err := cipher.Encrypt(ProviderOpenAICompatible, oldBaseURL, []byte("existing-key"))
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{
		runtime: StoredRuntime{Enabled: true, ActiveProvider: ProviderOpenAICompatible},
		providers: map[string]StoredProvider{
			ProviderOpenAICompatible: {
				Provider: ProviderOpenAICompatible, BaseURL: oldBaseURL,
				Model: "old-model", APICiphertext: existing,
			},
		},
	}
	service := newTestService(t, store)
	_, err = service.Update(context.Background(), 7, UpdateInput{
		Enabled: true, ActiveProvider: ProviderOpenAICompatible,
		BaseURL: newBaseURL, Model: "new-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := service.ForJob(context.Background(), ProviderOpenAICompatible, "recorded-model")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.BaseURL != newBaseURL || runtime.APIKey != "existing-key" {
		t.Fatalf("runtime = %#v", runtime)
	}
}

func TestUpdateBaseURLDoesNotOverwriteConcurrentKeyRotation(t *testing.T) {
	const oldBaseURL = "https://old.example/v1"
	const newBaseURL = "https://new.example/v1"
	cipher, err := NewCipher(strings.Repeat("m", 32))
	if err != nil {
		t.Fatal(err)
	}
	oldCiphertext, err := cipher.Encrypt(ProviderOpenAICompatible, oldBaseURL, []byte("old-key"))
	if err != nil {
		t.Fatal(err)
	}
	newCiphertext, err := cipher.Encrypt(ProviderOpenAICompatible, oldBaseURL, []byte("rotated-key"))
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{
		runtime: StoredRuntime{Enabled: true, ActiveProvider: ProviderOpenAICompatible},
		providers: map[string]StoredProvider{
			ProviderOpenAICompatible: {
				Provider: ProviderOpenAICompatible, BaseURL: oldBaseURL,
				Model: "old-model", APICiphertext: oldCiphertext,
			},
		},
		beforeUpdate: func(store *fakeStore) {
			provider := store.providers[ProviderOpenAICompatible]
			provider.APICiphertext = newCiphertext
			store.providers[ProviderOpenAICompatible] = provider
		},
	}
	_, err = newTestService(t, store).Update(context.Background(), 7, UpdateInput{
		Enabled: true, ActiveProvider: ProviderOpenAICompatible,
		BaseURL: newBaseURL, Model: "new-model",
	})
	if !errors.Is(err, ErrSettingsConflict) {
		t.Fatalf("Update() error = %v, want %v", err, ErrSettingsConflict)
	}
	if !bytes.Equal(store.providers[ProviderOpenAICompatible].APICiphertext, newCiphertext) {
		t.Fatal("concurrent key rotation was overwritten")
	}
}

func TestUpdatePreservedKeyDoesNotRestoreStaleBaseURL(t *testing.T) {
	const originalBaseURL = "https://original.example/v1"
	const concurrentBaseURL = "https://concurrent.example/v1"
	cipher, err := NewCipher(strings.Repeat("m", 32))
	if err != nil {
		t.Fatal(err)
	}
	originalCiphertext, err := cipher.Encrypt(ProviderOpenAICompatible, originalBaseURL, []byte("original-key"))
	if err != nil {
		t.Fatal(err)
	}
	concurrentCiphertext, err := cipher.Encrypt(ProviderOpenAICompatible, concurrentBaseURL, []byte("concurrent-key"))
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{
		runtime: StoredRuntime{Enabled: true, ActiveProvider: ProviderOpenAICompatible},
		providers: map[string]StoredProvider{
			ProviderOpenAICompatible: {
				Provider: ProviderOpenAICompatible, BaseURL: originalBaseURL,
				Model: "original-model", APICiphertext: originalCiphertext,
			},
		},
		beforeUpdate: func(store *fakeStore) {
			provider := store.providers[ProviderOpenAICompatible]
			provider.BaseURL = concurrentBaseURL
			provider.APICiphertext = concurrentCiphertext
			store.providers[ProviderOpenAICompatible] = provider
		},
	}
	_, err = newTestService(t, store).Update(context.Background(), 7, UpdateInput{
		Enabled: true, ActiveProvider: ProviderOpenAICompatible,
		BaseURL: originalBaseURL, Model: "original-model",
	})
	if !errors.Is(err, ErrSettingsConflict) {
		t.Fatalf("Update() error = %v, want %v", err, ErrSettingsConflict)
	}
	saved := store.providers[ProviderOpenAICompatible]
	if saved.BaseURL != concurrentBaseURL || !bytes.Equal(saved.APICiphertext, concurrentCiphertext) {
		t.Fatalf("concurrent credential pair was split: %#v", saved)
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

func TestConnectionUsesSavedKeyWhileAIIsDisabled(t *testing.T) {
	cipher, err := NewCipher(strings.Repeat("m", 32))
	if err != nil {
		t.Fatal(err)
	}
	secret, err := cipher.Encrypt(ProviderZhipu, zhipuBaseURL, []byte("saved-key"))
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{
		runtime: StoredRuntime{Enabled: false, ActiveProvider: ProviderZhipu},
		providers: map[string]StoredProvider{
			ProviderZhipu: {Provider: ProviderZhipu, BaseURL: zhipuBaseURL, Model: "glm-5.3-flash", APICiphertext: secret},
		},
	}
	tester := &fakeConnectionTester{}
	service, err := New(store, strings.Repeat("m", 32), func(context.Context, string) error { return nil }, WithConnectionTester(tester))
	if err != nil {
		t.Fatal(err)
	}

	result, err := service.TestConnection(context.Background(), TestInput{
		ActiveProvider: ProviderZhipu,
		Model:          "glm-5.3-flash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if tester.calls != 1 || tester.config.APIKey != "saved-key" || !tester.config.Enabled {
		t.Fatalf("tester calls=%d config=%#v", tester.calls, tester.config)
	}
	if result.Provider != ProviderZhipu || result.Model != "glm-5.3-flash" || !result.Success {
		t.Fatalf("result = %#v", result)
	}
}

func TestConnectionPrefersEnteredKeyWithoutSavingSettings(t *testing.T) {
	store := &fakeStore{providers: map[string]StoredProvider{}}
	tester := &fakeConnectionTester{}
	service, err := New(store, strings.Repeat("m", 32), func(context.Context, string) error { return nil }, WithConnectionTester(tester))
	if err != nil {
		t.Fatal(err)
	}

	_, err = service.TestConnection(context.Background(), TestInput{
		ActiveProvider: ProviderOpenAI,
		Model:          "gpt-5-mini",
		APIKey:         "new-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	if tester.config.APIKey != "new-key" {
		t.Fatalf("tester key = %q", tester.config.APIKey)
	}
	if store.update.Provider != "" || len(store.providers) != 0 {
		t.Fatalf("test connection changed persisted settings: update=%#v providers=%#v", store.update, store.providers)
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

func TestUpdateCanDisableAIWithoutDecryptingStoredCredential(t *testing.T) {
	store := &fakeStore{
		runtime: StoredRuntime{Enabled: true, ActiveProvider: ProviderOpenAI},
		providers: map[string]StoredProvider{
			ProviderOpenAI: {
				Provider: ProviderOpenAI, BaseURL: openAIBaseURL,
				Model: "gpt-5-mini", APICiphertext: []byte("damaged-ciphertext"),
			},
		},
	}
	_, err := newTestService(t, store).Update(context.Background(), 7, UpdateInput{
		Enabled: false, ActiveProvider: ProviderOpenAI, Model: "gpt-5-mini",
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.runtime.Enabled || !store.update.PreserveKey {
		t.Fatalf("runtime = %#v update = %#v", store.runtime, store.update)
	}
}

func TestUpdateCanDisableCustomProviderDuringDNSFailure(t *testing.T) {
	const baseURL = "https://llm.example.com/v1"
	store := &fakeStore{
		runtime: StoredRuntime{Enabled: true, ActiveProvider: ProviderOpenAICompatible},
		providers: map[string]StoredProvider{
			ProviderOpenAICompatible: {
				Provider: ProviderOpenAICompatible, BaseURL: baseURL,
				Model: "custom-model", APICiphertext: []byte("damaged-ciphertext"),
			},
		},
	}
	service, err := New(store, strings.Repeat("m", 32), func(context.Context, string) error {
		return errors.New("DNS unavailable")
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Update(context.Background(), 7, UpdateInput{
		Enabled: false, ActiveProvider: ProviderOpenAICompatible,
		BaseURL: baseURL, Model: "custom-model",
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.runtime.Enabled || !store.update.PreserveKey {
		t.Fatalf("runtime = %#v update = %#v", store.runtime, store.update)
	}
}

func TestForJobUsesRecordedModelAndLatestKey(t *testing.T) {
	cipher, err := NewCipher(strings.Repeat("m", 32))
	if err != nil {
		t.Fatal(err)
	}
	secret, err := cipher.Encrypt(ProviderOpenAI, openAIBaseURL, []byte("latest-key"))
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

func TestForJobRejectsStoredBaseURLThatFailsRuntimeValidation(t *testing.T) {
	const unsafeBaseURL = "http://93.184.216.34:8080/v1"
	cipher, err := NewCipher(strings.Repeat("m", 32))
	if err != nil {
		t.Fatal(err)
	}
	secret, err := cipher.Encrypt(ProviderOpenAICompatible, unsafeBaseURL, []byte("sensitive-key"))
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{
		runtime: StoredRuntime{Enabled: true, ActiveProvider: ProviderOpenAICompatible},
		providers: map[string]StoredProvider{
			ProviderOpenAICompatible: {
				Provider: ProviderOpenAICompatible, BaseURL: unsafeBaseURL,
				Model: "custom-model", APICiphertext: secret,
			},
		},
	}
	service, err := New(store, strings.Repeat("m", 32), func(_ context.Context, raw string) error {
		if strings.HasPrefix(raw, "https://") {
			return nil
		}
		return &unsafeValidationError{message: "HTTPS required"}
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ForJob(context.Background(), ProviderOpenAICompatible, "recorded-model")
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("ForJob() error = %v, want %v", err, ErrNotConfigured)
	}
}

func TestForJobPropagatesTransientURLValidationFailure(t *testing.T) {
	const baseURL = "https://llm.example.com/v1"
	dnsErr := errors.New("DNS temporarily unavailable")
	cipher, err := NewCipher(strings.Repeat("m", 32))
	if err != nil {
		t.Fatal(err)
	}
	secret, err := cipher.Encrypt(ProviderOpenAICompatible, baseURL, []byte("sensitive-key"))
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{
		runtime: StoredRuntime{Enabled: true, ActiveProvider: ProviderOpenAICompatible},
		providers: map[string]StoredProvider{
			ProviderOpenAICompatible: {
				Provider: ProviderOpenAICompatible, BaseURL: baseURL,
				Model: "custom-model", APICiphertext: secret,
			},
		},
	}
	service, err := New(store, strings.Repeat("m", 32), func(context.Context, string) error {
		return dnsErr
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.ForJob(context.Background(), ProviderOpenAICompatible, "recorded-model")
	if !errors.Is(err, dnsErr) {
		t.Fatalf("ForJob() error = %v, want resolver error", err)
	}
}

func TestActiveReturnsNotConfiguredWhenDisabled(t *testing.T) {
	store := &fakeStore{runtime: StoredRuntime{Enabled: false, ActiveProvider: ProviderZhipu}, providers: map[string]StoredProvider{}}
	_, err := newTestService(t, store).Active(context.Background())
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Active() error = %v, want %v", err, ErrNotConfigured)
	}
}
