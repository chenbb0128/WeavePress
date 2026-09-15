package aisettings

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type URLValidator func(context.Context, string) error

type Service struct {
	store       Store
	cipher      *Cipher
	validateURL URLValidator
}

func New(store Store, mediaSigningKey string, validateURL URLValidator) (*Service, error) {
	cipher, err := NewCipher(mediaSigningKey)
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, fmt.Errorf("%w: store is required", ErrInvalidSettings)
	}
	return &Service{store: store, cipher: cipher, validateURL: validateURL}, nil
}

func (s *Service) View(ctx context.Context) (SettingsView, error) {
	runtime, err := s.store.GetRuntime(ctx)
	if err != nil {
		return SettingsView{}, err
	}
	stored, err := s.store.ListProviders(ctx)
	if err != nil {
		return SettingsView{}, err
	}
	byID := make(map[string]StoredProvider, len(stored))
	for _, item := range stored {
		byID[item.Provider] = item
	}
	views := make([]ProviderView, 0, len(providerCatalog))
	for _, definition := range providerCatalog {
		item, exists := byID[definition.ID]
		baseURL, model := definition.BaseURL, definition.DefaultModel
		if exists {
			if definition.BaseURLEditable {
				baseURL = item.BaseURL
			}
			if strings.TrimSpace(item.Model) != "" {
				model = item.Model
			}
		}
		views = append(views, ProviderView{
			ID: definition.ID, Name: definition.Name, BaseURL: baseURL,
			BaseURLEditable: definition.BaseURLEditable, Model: model,
			ModelOptions:  append([]string(nil), definition.ModelOptions...),
			KeyConfigured: exists && len(item.APICiphertext) > 0,
		})
	}
	return SettingsView{Enabled: runtime.Enabled, ActiveProvider: runtime.ActiveProvider, Providers: views}, nil
}

func (s *Service) Update(ctx context.Context, userID uint64, input UpdateInput) (SettingsView, error) {
	definition, ok := providerDefinition(strings.TrimSpace(input.ActiveProvider))
	if !ok || userID == 0 {
		return SettingsView{}, fmt.Errorf("%w: provider is invalid", ErrInvalidSettings)
	}
	model := strings.TrimSpace(input.Model)
	if model == "" {
		model = definition.DefaultModel
	}
	if model == "" || len(model) > 100 {
		return SettingsView{}, fmt.Errorf("%w: model is required", ErrInvalidSettings)
	}
	baseURL := definition.BaseURL
	if definition.BaseURLEditable {
		baseURL = strings.TrimSpace(input.BaseURL)
		if baseURL == "" || s.validateURL == nil {
			return SettingsView{}, fmt.Errorf("%w: base URL is invalid", ErrInvalidSettings)
		}
	}
	apiKey := strings.TrimSpace(input.APIKey)
	preserveKey := apiKey == ""
	compareCredential := false
	var expectedBaseURL string
	var expectedCiphertext []byte
	var stored StoredProvider
	storedFound := false
	if preserveKey {
		var err error
		stored, err = s.store.GetProvider(ctx, definition.ID)
		if err != nil && !errors.Is(err, ErrProviderNotConfigured) {
			return SettingsView{}, err
		}
		storedFound = err == nil
		if storedFound {
			compareCredential = true
			expectedBaseURL = stored.BaseURL
			expectedCiphertext = append([]byte(nil), stored.APICiphertext...)
		}
	}
	if definition.BaseURLEditable {
		emergencyDisable := !input.Enabled && preserveKey && storedFound && stored.BaseURL == baseURL
		if !emergencyDisable && s.validateURL(ctx, baseURL) != nil {
			return SettingsView{}, fmt.Errorf("%w: base URL is invalid", ErrInvalidSettings)
		}
	}
	var ciphertext []byte
	if !preserveKey {
		if len(apiKey) > 2048 {
			return SettingsView{}, fmt.Errorf("%w: API key is too long", ErrInvalidSettings)
		}
		var err error
		ciphertext, err = s.cipher.Encrypt(definition.ID, baseURL, []byte(apiKey))
		if err != nil {
			return SettingsView{}, err
		}
	}
	if preserveKey {
		if !storedFound {
			if input.Enabled {
				return SettingsView{}, fmt.Errorf("%w: API key is required", ErrInvalidSettings)
			}
		} else if len(stored.APICiphertext) == 0 {
			if input.Enabled {
				return SettingsView{}, fmt.Errorf("%w: API key is required", ErrInvalidSettings)
			}
		} else if input.Enabled || stored.BaseURL != baseURL {
			plain, err := s.cipher.Decrypt(stored.Provider, stored.BaseURL, stored.APICiphertext)
			if err != nil {
				return SettingsView{}, err
			}
			defer clear(plain)
			if stored.BaseURL != baseURL {
				ciphertext, err = s.cipher.Encrypt(definition.ID, baseURL, plain)
				if err != nil {
					return SettingsView{}, err
				}
				preserveKey = false
			}
		}
	}
	if err := s.store.Update(ctx, StoreUpdate{
		Enabled: input.Enabled, ActiveProvider: definition.ID, Provider: definition.ID,
		BaseURL: baseURL, Model: model, APICiphertext: ciphertext,
		PreserveKey: preserveKey, CompareCredential: compareCredential,
		ExpectedBaseURL: expectedBaseURL, ExpectedAPICiphertext: expectedCiphertext,
		UpdatedBy: userID,
	}); err != nil {
		return SettingsView{}, err
	}
	return s.View(ctx)
}

func (s *Service) Active(ctx context.Context) (RuntimeConfig, error) {
	runtime, err := s.store.GetRuntime(ctx)
	if err != nil {
		return RuntimeConfig{}, err
	}
	if !runtime.Enabled {
		return RuntimeConfig{}, ErrNotConfigured
	}
	provider, err := s.store.GetProvider(ctx, runtime.ActiveProvider)
	if err != nil {
		if errors.Is(err, ErrProviderNotConfigured) {
			return RuntimeConfig{}, ErrNotConfigured
		}
		return RuntimeConfig{}, err
	}
	return s.runtimeConfig(ctx, provider, provider.Model)
}

func (s *Service) Status(ctx context.Context) (RuntimeStatus, error) {
	runtime, err := s.store.GetRuntime(ctx)
	if err != nil {
		return RuntimeStatus{}, err
	}
	definition, ok := providerDefinition(runtime.ActiveProvider)
	if !ok {
		return RuntimeStatus{}, ErrNotConfigured
	}
	model := definition.DefaultModel
	provider, err := s.store.GetProvider(ctx, runtime.ActiveProvider)
	if err == nil && strings.TrimSpace(provider.Model) != "" {
		model = provider.Model
	} else if err != nil && !errors.Is(err, ErrProviderNotConfigured) {
		return RuntimeStatus{}, err
	}
	return RuntimeStatus{Enabled: runtime.Enabled, Provider: runtime.ActiveProvider, Model: model}, nil
}

func (s *Service) ForJob(ctx context.Context, providerID, model string) (RuntimeConfig, error) {
	runtime, err := s.store.GetRuntime(ctx)
	if err != nil {
		return RuntimeConfig{}, err
	}
	if !runtime.Enabled {
		return RuntimeConfig{}, ErrNotConfigured
	}
	provider, err := s.store.GetProvider(ctx, providerID)
	if err != nil {
		if errors.Is(err, ErrProviderNotConfigured) {
			return RuntimeConfig{}, ErrNotConfigured
		}
		return RuntimeConfig{}, err
	}
	return s.runtimeConfig(ctx, provider, model)
}

func (s *Service) runtimeConfig(ctx context.Context, provider StoredProvider, model string) (RuntimeConfig, error) {
	definition, ok := providerDefinition(provider.Provider)
	if !ok || strings.TrimSpace(model) == "" || strings.TrimSpace(provider.BaseURL) == "" || len(provider.APICiphertext) == 0 {
		return RuntimeConfig{}, ErrNotConfigured
	}
	if !definition.BaseURLEditable && provider.BaseURL != definition.BaseURL {
		return RuntimeConfig{}, ErrNotConfigured
	}
	if s.validateURL == nil {
		return RuntimeConfig{}, ErrNotConfigured
	}
	if err := s.validateURL(ctx, provider.BaseURL); err != nil {
		var unsafe interface{ UnsafeURL() bool }
		if errors.As(err, &unsafe) && unsafe.UnsafeURL() {
			return RuntimeConfig{}, ErrNotConfigured
		}
		return RuntimeConfig{}, err
	}
	plain, err := s.cipher.Decrypt(provider.Provider, provider.BaseURL, provider.APICiphertext)
	if err != nil {
		return RuntimeConfig{}, err
	}
	if strings.TrimSpace(string(plain)) == "" {
		return RuntimeConfig{}, ErrNotConfigured
	}
	return RuntimeConfig{Enabled: true, Provider: provider.Provider, BaseURL: provider.BaseURL, Model: strings.TrimSpace(model), APIKey: string(plain)}, nil
}

func providerDefinition(id string) (ProviderDefinition, bool) {
	for _, definition := range providerCatalog {
		if definition.ID == id {
			return definition, true
		}
	}
	return ProviderDefinition{}, false
}
