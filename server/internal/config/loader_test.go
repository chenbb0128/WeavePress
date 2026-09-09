package config

import "testing"

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
