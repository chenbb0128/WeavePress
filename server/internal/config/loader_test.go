package config

import (
	"strings"
	"testing"
)

func TestDefaultWorkerQueuesIncludeAI(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"collection": 2, "ai": 1, "publishing": 1, "default": 1}
	for queue, weight := range want {
		if cfg.Worker.Queues[queue] != weight {
			t.Fatalf("worker queue %q weight = %d, want %d; queues=%#v", queue, cfg.Worker.Queues[queue], weight, cfg.Worker.Queues)
		}
	}
}

func TestLoadIgnoresLegacyAIEnvironmentVariables(t *testing.T) {
	t.Setenv("WEAVEPRESS_AI_ENABLED", "true")
	t.Setenv("WEAVEPRESS_AI_API_KEY", "must-not-load")
	t.Setenv("WEAVEPRESS_AI_REQUEST_TIMEOUT", "1s")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	for key := range cfg.SanitizedSummary() {
		if strings.HasPrefix(key, "ai_") {
			t.Fatalf("legacy AI config remains: %s", key)
		}
	}
}
