package worker

import (
	"reflect"
	"testing"

	"cscan/internal/scanner"
	"cscan/internal/scheduler"
)

// **Validates: Requirements 2.4, 3.4, 3.5**
func TestBuildDirScanOptionsFieldMappingContract(t *testing.T) {
	paths := []string{"/admin", "/api/v1", "/health"}
	config := &scheduler.DirScanConfig{
		Extensions:      []string{"php", ".js"},
		StatusCodes:     []int{503, 200, 503, 418},
		FollowRedirect:  true,
		AutoCalibration: true,
		FilterSize:      "123,456",
		FilterWords:     "10",
		FilterLines:     "20",
		FilterRegex:     "soft-404",
		MatcherMode:     "and",
		FilterMode:      "or",
		Recursion:       true,
		RecursionDepth:  7,
	}

	got := buildDirScanOptions(paths, 37, 14, 81, config)
	want := &scanner.FFufOptions{
		Paths:           paths,
		Threads:         37,
		Timeout:         14,
		Extensions:      config.Extensions,
		StatusCodes:     config.StatusCodes,
		FollowRedirect:  true,
		AutoCalibration: true,
		FilterSize:      "123,456",
		FilterWords:     "10",
		FilterLines:     "20",
		FilterRegex:     "soft-404",
		MatcherMode:     "and",
		FilterMode:      "or",
		Rate:            81,
		Recursion:       true,
		RecursionDepth:  7,
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("buildDirScanOptions()=%+v, want direct field mapping %+v", got, want)
	}
	if !reflect.DeepEqual(got.StatusCodes, []int{503, 200, 503, 418}) {
		t.Fatalf("StatusCodes=%v, want original order, duplicates, and values preserved", got.StatusCodes)
	}
}

// **Validates: Requirements 2.4, 3.1, 3.5**
func TestBuildDirScanOptionsDoesNotSelectDefaultStatusCodes(t *testing.T) {
	for _, tt := range []struct {
		name        string
		statusCodes []int
		wantNil     bool
	}{
		{name: "nil remains nil", statusCodes: nil, wantNil: true},
		{name: "empty remains empty", statusCodes: []int{}, wantNil: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDirScanOptions(nil, 1, 2, 0, &scheduler.DirScanConfig{StatusCodes: tt.statusCodes})
			if len(got.StatusCodes) != 0 {
				t.Fatalf("StatusCodes=%v, want no Worker-selected default", got.StatusCodes)
			}
			if (got.StatusCodes == nil) != tt.wantNil {
				t.Fatalf("StatusCodes nil=%t, want %t", got.StatusCodes == nil, tt.wantNil)
			}
		})
	}
}
