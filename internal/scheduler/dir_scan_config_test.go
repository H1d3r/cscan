package scheduler

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// **Validates: Requirements 2.4, 3.3, 3.5**
func TestDirScanParseTaskConfigContract(t *testing.T) {
	tests := []struct {
		name                    string
		configJSON              string
		expectedStatusCodes     []int
		expectedAutoCalibration bool
		expectNilStatusCodes    bool
	}{
		{
			name:                    "ordinary task preserves custom values",
			configJSON:              `{"dirscan":{"enable":true,"tool":"ffuf","statusCodes":[200,302],"autoCalibration":true}}`,
			expectedStatusCodes:     []int{200, 302},
			expectedAutoCalibration: true,
		},
		{
			name:                    "scheduled task preserves custom values",
			configJSON:              `{"portscan":{"enable":true},"dirscan":{"enable":true,"statusCodes":[200,302],"autoCalibration":true}}`,
			expectedStatusCodes:     []int{200, 302},
			expectedAutoCalibration: true,
		},
		{
			name:                    "explicit empty status codes and false auto calibration",
			configJSON:              `{"dirscan":{"statusCodes":[],"autoCalibration":false}}`,
			expectedStatusCodes:     []int{},
			expectedAutoCalibration: false,
		},
		{
			name:                    "omitted status codes and false auto calibration",
			configJSON:              `{"dirscan":{"autoCalibration":false}}`,
			expectedStatusCodes:     nil,
			expectedAutoCalibration: false,
			expectNilStatusCodes:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := ParseTaskConfig(tt.configJSON)
			if err != nil {
				t.Fatalf("ParseTaskConfig() error: %v", err)
			}
			if config.DirScan == nil {
				t.Fatal("ParseTaskConfig() did not populate dirscan")
			}
			if !reflect.DeepEqual(config.DirScan.StatusCodes, tt.expectedStatusCodes) {
				t.Fatalf("StatusCodes=%v, want %v", config.DirScan.StatusCodes, tt.expectedStatusCodes)
			}
			if tt.expectNilStatusCodes != (config.DirScan.StatusCodes == nil) {
				t.Fatalf("StatusCodes nil=%t, want %t", config.DirScan.StatusCodes == nil, tt.expectNilStatusCodes)
			}
			if config.DirScan.AutoCalibration != tt.expectedAutoCalibration {
				t.Fatalf("AutoCalibration=%t, want %t", config.DirScan.AutoCalibration, tt.expectedAutoCalibration)
			}
		})
	}
}

// **Validates: Requirements 2.4, 3.3, 3.5**
func TestScanTemplateDirScanCompatibility(t *testing.T) {
	for _, templateName := range []string{"quick-scan.json", "standard-scan.json"} {
		t.Run(templateName, func(t *testing.T) {
			templatePath := filepath.Join("..", "..", "rules", "scan-template", templateName)
			contents, err := os.ReadFile(templatePath)
			if err != nil {
				t.Fatalf("read existing scan template %q: %v", templatePath, err)
			}

			config, err := ParseTaskConfig(string(contents))
			if err != nil {
				t.Fatalf("ParseTaskConfig(existing template) error: %v", err)
			}
			if config.DirScan == nil {
				t.Fatal("existing template did not populate dirscan")
			}
			if config.DirScan.StatusCodes == nil {
				t.Fatal("explicit template statusCodes:[] became nil")
			}
			if len(config.DirScan.StatusCodes) != 0 {
				t.Fatalf("template StatusCodes=%v, want zero length", config.DirScan.StatusCodes)
			}
			if !config.DirScan.AutoCalibration {
				t.Fatal("template AutoCalibration=false, want existing true value")
			}
		})
	}
}
