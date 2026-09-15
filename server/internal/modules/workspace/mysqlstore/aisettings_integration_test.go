//go:build integration

package mysqlstore_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/chenbb0128/weavepress/server/internal/modules/aisettings"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace/mysqlstore"
)

func TestAISettingsStorePreservesExistingKey(t *testing.T) {
	db, _ := layoutTestDatabase(t, 20260915000100)
	if _, err := db.Exec(`INSERT INTO users (id, username, password_hash) VALUES (1, 'ai-settings', 'unused')`); err != nil {
		t.Fatal(err)
	}
	store := mysqlstore.NewAISettingsStore(db)
	ctx := context.Background()
	input := aisettings.StoreUpdate{
		Enabled: true, ActiveProvider: aisettings.ProviderZhipu,
		Provider: aisettings.ProviderZhipu, BaseURL: "https://open.bigmodel.cn/api/paas/v4",
		Model: "glm-5.3-flash", APICiphertext: []byte("cipher-one"), UpdatedBy: 1,
	}
	if err := store.Update(ctx, input); err != nil {
		t.Fatal(err)
	}
	input.Model = "glm-custom"
	input.APICiphertext = nil
	input.PreserveKey = true
	if err := store.Update(ctx, input); err != nil {
		t.Fatal(err)
	}
	saved, err := store.GetProvider(ctx, aisettings.ProviderZhipu)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Model != "glm-custom" || !bytes.Equal(saved.APICiphertext, []byte("cipher-one")) {
		t.Fatalf("saved = %#v", saved)
	}
}
