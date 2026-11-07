package config

import (
	"os"
	"testing"
)

func TestGatewayOnlyDefaults(t *testing.T) {
	t.Setenv("GATEWAY_ONLY", "true")
	t.Setenv("GATEWAY_NODE_ID", "gw-test-edge")
	t.Setenv("DB_PATH", "/tmp/test.db")
	// Clear concurrency overrides so defaults apply.
	for _, k := range []string{
		"LARGE_GROUP_CONCURRENCY",
		"SMALL_GROUP_CONCURRENCY",
		"QUEUE_MAX_SIZE",
		"WS_BACKPRESSURE_BYTES",
	} {
		_ = os.Unsetenv(k)
	}

	cfg := Load()
	if !cfg.GatewayOnly {
		t.Fatal("expected GatewayOnly=true")
	}
	if cfg.GatewayID != "gw-test-edge" {
		t.Fatalf("GatewayID=%q", cfg.GatewayID)
	}
	if cfg.LargeGroupConcurrency != 8 || cfg.SmallGroupConcurrency != 40 {
		t.Fatalf("slim concurrency not applied: large=%d small=%d",
			cfg.LargeGroupConcurrency, cfg.SmallGroupConcurrency)
	}
	if cfg.QueueMaxSize != 10000 || cfg.WsBackpressureBytes != 32768 {
		t.Fatalf("slim queue/backpressure not applied: queue=%d bp=%d",
			cfg.QueueMaxSize, cfg.WsBackpressureBytes)
	}
}

func TestGetEnvBool(t *testing.T) {
	cases := map[string]bool{
		"1": true, "true": true, "YES": true, "on": true,
		"0": false, "false": false, "NO": false, "off": false,
	}
	for raw, want := range cases {
		t.Setenv("GATEWAY_ONLY", raw)
		if got := getEnvBool("GATEWAY_ONLY", false); got != want {
			t.Fatalf("%q => %v, want %v", raw, got, want)
		}
	}
}
