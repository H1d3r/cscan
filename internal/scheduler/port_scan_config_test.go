package scheduler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPortScanConfigTimeoutCompatibility(t *testing.T) {
	tests := []struct {
		name               string
		configJSON         string
		wantTargetTimeout  int
		wantProbeTimeoutMs int
	}{
		{
			name:               "defaults",
			configJSON:         `{"portscan":{"enable":true}}`,
			wantTargetTimeout:  120,
			wantProbeTimeoutMs: 1000,
		},
		{
			name:               "new fields",
			configJSON:         `{"portscan":{"targetTimeout":90,"probeTimeoutMs":750}}`,
			wantTargetTimeout:  90,
			wantProbeTimeoutMs: 750,
		},
		{
			name:               "legacy timeout becomes target timeout",
			configJSON:         `{"portscan":{"timeout":120}}`,
			wantTargetTimeout:  120,
			wantProbeTimeoutMs: 1000,
		},
		{
			name:               "new target timeout takes precedence",
			configJSON:         `{"portscan":{"targetTimeout":90,"probeTimeoutMs":500,"timeout":120}}`,
			wantTargetTimeout:  90,
			wantProbeTimeoutMs: 500,
		},
		{
			name:               "zero new value falls back to legacy",
			configJSON:         `{"portscan":{"targetTimeout":0,"probeTimeoutMs":0,"timeout":45}}`,
			wantTargetTimeout:  45,
			wantProbeTimeoutMs: 1000,
		},
		{
			name:               "obsolete aggregate timeout is ignored",
			configJSON:         `{"portscan":{"aggregatedTimeout":999}}`,
			wantTargetTimeout:  120,
			wantProbeTimeoutMs: 1000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := ParseTaskConfig(tt.configJSON)
			if err != nil {
				t.Fatalf("ParseTaskConfig() error: %v", err)
			}
			if config.PortScan == nil {
				t.Fatal("ParseTaskConfig() did not populate portscan")
			}
			if config.PortScan.TargetTimeout != tt.wantTargetTimeout {
				t.Fatalf("TargetTimeout=%d, want %d", config.PortScan.TargetTimeout, tt.wantTargetTimeout)
			}
			if config.PortScan.ProbeTimeoutMs != tt.wantProbeTimeoutMs {
				t.Fatalf("ProbeTimeoutMs=%d, want %d", config.PortScan.ProbeTimeoutMs, tt.wantProbeTimeoutMs)
			}
			if config.PortScan.LegacyTimeout != 0 {
				t.Fatalf("LegacyTimeout=%d, want 0 after normalization", config.PortScan.LegacyTimeout)
			}
		})
	}
}

func TestPortScanConfigRetriesPreservesMissingVersusExplicitZero(t *testing.T) {
	for _, tt := range []struct {
		name       string
		configJSON string
		want       int
	}{
		{name: "missing uses default", configJSON: `{"portscan":{}}`, want: 2},
		{name: "explicit zero is preserved", configJSON: `{"portscan":{"retries":0}}`, want: 0},
		{name: "explicit value is preserved", configJSON: `{"portscan":{"retries":4}}`, want: 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			config, err := ParseTaskConfig(tt.configJSON)
			if err != nil {
				t.Fatalf("ParseTaskConfig() error: %v", err)
			}
			if config.PortScan == nil {
				t.Fatal("ParseTaskConfig() did not populate portscan")
			}
			if config.PortScan.Retries != tt.want {
				t.Fatalf("Retries=%d, want %d", config.PortScan.Retries, tt.want)
			}
		})
	}
}

func TestPortScanConfigRejectsNegativeTimeouts(t *testing.T) {
	for _, tt := range []struct {
		name       string
		configJSON string
	}{
		{name: "target timeout", configJSON: `{"portscan":{"targetTimeout":-1}}`},
		{name: "probe timeout", configJSON: `{"portscan":{"probeTimeoutMs":-1}}`},
		{name: "legacy timeout", configJSON: `{"portscan":{"timeout":-1}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseTaskConfig(tt.configJSON); err == nil {
				t.Fatalf("ParseTaskConfig(%s) succeeded, want error", tt.configJSON)
			}
		})
	}
}

func TestPortScanConfigSerializationEmitsOnlyNewTimeoutFields(t *testing.T) {
	config, err := ParseTaskConfig(`{"portscan":{"timeout":120,"aggregatedTimeout":999}}`)
	if err != nil {
		t.Fatalf("ParseTaskConfig() error: %v", err)
	}
	// 即使程序化设置兼容字段，也必须保持 timeout 为只读输入，不得重新输出。
	config.PortScan.LegacyTimeout = 321
	serialized, err := BuildTaskConfig(config)
	if err != nil {
		t.Fatalf("BuildTaskConfig() error: %v", err)
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(serialized), &root); err != nil {
		t.Fatalf("unmarshal serialized task config: %v", err)
	}
	var portscan map[string]json.RawMessage
	if err := json.Unmarshal(root["portscan"], &portscan); err != nil {
		t.Fatalf("unmarshal serialized portscan config: %v", err)
	}
	if _, exists := portscan["timeout"]; exists {
		t.Fatalf("serialized portscan still contains legacy timeout: %s", serialized)
	}
	if _, exists := portscan["aggregatedTimeout"]; exists {
		t.Fatalf("serialized portscan still contains aggregatedTimeout: %s", serialized)
	}
	if _, exists := portscan["targetTimeout"]; !exists {
		t.Fatalf("serialized portscan is missing targetTimeout: %s", serialized)
	}
	if _, exists := portscan["probeTimeoutMs"]; !exists {
		t.Fatalf("serialized portscan is missing probeTimeoutMs: %s", serialized)
	}
}

func TestBuiltinTemplatesUseSplitPortScanTimeoutFields(t *testing.T) {
	for _, templateName := range []string{"quick-scan.json", "standard-scan.json"} {
		t.Run(templateName, func(t *testing.T) {
			templatePath := filepath.Join("..", "..", "rules", "scan-template", templateName)
			contents, err := os.ReadFile(templatePath)
			if err != nil {
				t.Fatalf("read scan template %q: %v", templatePath, err)
			}

			var root map[string]json.RawMessage
			if err := json.Unmarshal(contents, &root); err != nil {
				t.Fatalf("unmarshal scan template: %v", err)
			}
			var portscan map[string]json.RawMessage
			if err := json.Unmarshal(root["portscan"], &portscan); err != nil {
				t.Fatalf("unmarshal template portscan config: %v", err)
			}
			if _, exists := portscan["targetTimeout"]; !exists {
				t.Fatal("template portscan is missing targetTimeout")
			}
			if _, exists := portscan["probeTimeoutMs"]; !exists {
				t.Fatal("template portscan is missing probeTimeoutMs")
			}
			if _, exists := portscan["timeout"]; exists {
				t.Fatal("template portscan still contains legacy timeout")
			}
			if _, exists := portscan["aggregatedTimeout"]; exists {
				t.Fatal("template portscan still contains aggregatedTimeout")
			}
		})
	}
}
