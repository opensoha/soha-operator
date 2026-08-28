package main

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestZapOptionsWriteCanonicalJSON(t *testing.T) {
	var output bytes.Buffer
	options := newZapOptions()
	logger := zap.New(zap.UseFlagOptions(&options), zap.WriteTo(&output)).WithName("setup").WithValues("service", "soha-operator")
	logger.Info("starting manager", "event", "operator.manager.starting")

	var entry map[string]any
	if err := json.Unmarshal(output.Bytes(), &entry); err != nil {
		t.Fatalf("decode log entry: %v; output = %q", err, output.String())
	}
	if entry["level"] != "info" || entry["component"] != "setup" || entry["service"] != "soha-operator" || entry["event"] != "operator.manager.starting" || entry["message"] != "starting manager" {
		t.Fatalf("log fields = %#v", entry)
	}
	timestamp, ok := entry["timestamp"].(string)
	if !ok {
		t.Fatalf("timestamp = %#v", entry["timestamp"])
	}
	if parsed, err := time.Parse(time.RFC3339Nano, timestamp); err != nil || parsed.Location() != time.UTC {
		t.Fatalf("timestamp = %q, error = %v", timestamp, err)
	}
}
