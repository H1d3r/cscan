package scanner

import (
	"fmt"
	"math/rand"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/quick"
)

const ffufDefaultMatchCodesForExploration = "200,204,301,302,307,401,403,405,500"

type ffufBugConditionTestInput struct {
	Tool    string
	Options FFufOptions
}

type ffufBugConditionQuickInput struct {
	AutoCalibration bool
	StatusCodes     []int
}

func (ffufBugConditionQuickInput) Generate(r *rand.Rand, size int) reflect.Value {
	maxCodes := size
	if maxCodes < 1 {
		maxCodes = 1
	}
	if maxCodes > 8 {
		maxCodes = 8
	}

	statusCodes := make([]int, r.Intn(maxCodes)+1)
	for i := range statusCodes {
		statusCodes[i] = 100 + r.Intn(500)
	}

	return reflect.ValueOf(ffufBugConditionQuickInput{
		AutoCalibration: r.Intn(2) == 1,
		StatusCodes:     statusCodes,
	})
}

func isFFufBugCondition(input ffufBugConditionTestInput) bool {
	return input.Tool == "ffuf" && (input.Options.AutoCalibration || len(input.Options.StatusCodes) > 0)
}

func expectedFFufMatchCodes(statusCodes []int) string {
	if len(statusCodes) == 0 {
		return ffufDefaultMatchCodesForExploration
	}

	values := make([]string, len(statusCodes))
	for i, statusCode := range statusCodes {
		values[i] = strconv.Itoa(statusCode)
	}
	return strings.Join(values, ",")
}

func countFFufFlag(args []string, flag string) int {
	count := 0
	for _, arg := range args {
		if arg == flag {
			count++
		}
	}
	return count
}

func valueAfterFFufFlag(args []string, flag string) (string, bool) {
	for i, arg := range args {
		if arg == flag && i+1 < len(args) {
			return args[i+1], true
		}
	}
	return "", false
}

func checkFFufExpectedBehavior(input ffufBugConditionTestInput, args []string) error {
	matchCodeCount := countFFufFlag(args, "-mc")
	if matchCodeCount != 1 {
		return fmt.Errorf("COUNT_FLAG(args, \"-mc\")=%d, want 1; args=%q", matchCodeCount, args)
	}

	actualMatchCodes, ok := valueAfterFFufFlag(args, "-mc")
	if !ok {
		return fmt.Errorf("VALUE_AFTER_FLAG(args, \"-mc\") is missing; args=%q", args)
	}
	expectedMatchCodes := expectedFFufMatchCodes(input.Options.StatusCodes)
	if actualMatchCodes != expectedMatchCodes {
		return fmt.Errorf("VALUE_AFTER_FLAG(args, \"-mc\")=%q, want %q for StatusCodes=%v; args=%q", actualMatchCodes, expectedMatchCodes, input.Options.StatusCodes, args)
	}

	expectedAutoCalibrationCount := 0
	if input.Options.AutoCalibration {
		expectedAutoCalibrationCount = 1
	}
	actualAutoCalibrationCount := countFFufFlag(args, "-ac")
	if actualAutoCalibrationCount != expectedAutoCalibrationCount {
		return fmt.Errorf("COUNT_FLAG(args, \"-ac\")=%d, want %d for AutoCalibration=%t; args=%q", actualAutoCalibrationCount, expectedAutoCalibrationCount, input.Options.AutoCalibration, args)
	}

	return nil
}

// **Validates: Requirements 1.1, 1.2, 1.3, 1.4, 2.1, 2.2, 2.3, 2.4**
func TestFFufFrontendParametersBugCondition(t *testing.T) {
	tests := []struct {
		name            string
		autoCalibration bool
		statusCodes     []int
	}{
		{
			name:            "auto calibration with nil status codes",
			autoCalibration: true,
			statusCodes:     nil,
		},
		{
			name:            "custom status codes without auto calibration",
			autoCalibration: false,
			statusCodes:     []int{200, 302},
		},
		{
			name:            "combined auto calibration and custom status codes",
			autoCalibration: true,
			statusCodes:     []int{200, 302},
		},
		{
			name:            "single custom status code",
			autoCalibration: false,
			statusCodes:     []int{418},
		},
		{
			name:            "multiple custom status codes preserve order",
			autoCalibration: false,
			statusCodes:     []int{503, 201, 429},
		},
		{
			name:            "valid values are serialized without filtering or deduplication",
			autoCalibration: false,
			statusCodes:     []int{100, 200, 200, 599},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := ffufBugConditionTestInput{
				Tool: "ffuf",
				Options: FFufOptions{
					Threads:         20,
					Timeout:         10,
					AutoCalibration: tt.autoCalibration,
					StatusCodes:     tt.statusCodes,
				},
			}
			if !isFFufBugCondition(input) {
				t.Fatalf("test input does not satisfy isBugCondition: %+v", input)
			}
			if err := input.Options.Validate(); err != nil {
				t.Fatalf("Validate() rejected valid options: %v", err)
			}

			args := buildFFufArgs("https://example.test/FUZZ", "wordlist.txt", "output.json", &input.Options)
			if err := checkFFufExpectedBehavior(input, args); err != nil {
				t.Error(err)
			}
		})
	}

	for _, statusCode := range []int{99, 600} {
		opts := &FFufOptions{StatusCodes: []int{statusCode}}
		if err := opts.Validate(); err == nil {
			t.Errorf("Validate() accepted invalid status code %d", statusCode)
		}
	}
}

// **Validates: Requirements 1.1, 1.2, 1.3, 2.1, 2.2, 2.3**
func TestFFufStatusCodesAndAutoCalibrationBugConditionProperty(t *testing.T) {
	property := func(generated ffufBugConditionQuickInput) bool {
		input := ffufBugConditionTestInput{
			Tool: "ffuf",
			Options: FFufOptions{
				Threads:         20,
				Timeout:         10,
				AutoCalibration: generated.AutoCalibration,
				StatusCodes:     generated.StatusCodes,
			},
		}
		if !isFFufBugCondition(input) {
			return false
		}

		args := buildFFufArgs("https://example.test/FUZZ", "wordlist.txt", "output.json", &input.Options)
		return checkFFufExpectedBehavior(input, args) == nil
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("FFUF bug condition counterexample: %v", err)
	}
}

type ffufPreservationQuickInput struct {
	FollowRedirect       bool
	Recursion            bool
	RecursionDepth       int
	Rate                 int
	Extensions           []string
	AutoCalibration      bool
	UseCustomStatusCodes bool
}

func (ffufPreservationQuickInput) Generate(r *rand.Rand, _ int) reflect.Value {
	extensionCandidates := []string{"php", ".js", "..bak", ""}
	extensionCount := r.Intn(4)
	extensions := make([]string, extensionCount)
	for i := range extensions {
		extensions[i] = extensionCandidates[r.Intn(len(extensionCandidates))]
	}

	return reflect.ValueOf(ffufPreservationQuickInput{
		FollowRedirect:       r.Intn(2) == 1,
		Recursion:            r.Intn(2) == 1,
		RecursionDepth:       r.Intn(6),
		Rate:                 r.Intn(102) - 1,
		Extensions:           extensions,
		AutoCalibration:      r.Intn(2) == 1,
		UseCustomStatusCodes: r.Intn(2) == 1,
	})
}

func ffufUnrelatedArgsProjection(args []string) []string {
	projection := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-mc":
			i++
		case "-ac":
		default:
			projection = append(projection, args[i])
		}
	}
	return projection
}

func expectedFFufUnrelatedArgs(baseURL, wordlistFile, outputPath string, opts *FFufOptions) []string {
	args := []string{
		"-u", baseURL,
		"-w", wordlistFile,
		"-of", "json",
		"-t", strconv.Itoa(opts.Threads),
		"-timeout", strconv.Itoa(opts.Timeout),
	}
	if opts.FollowRedirect {
		args = append(args, "-r")
	}
	if opts.Recursion {
		args = append(args, "-recursion", "-recursion-depth", strconv.Itoa(opts.RecursionDepth))
	}
	if opts.Rate > 0 {
		args = append(args, "-rate", strconv.Itoa(opts.Rate))
	}
	for _, extension := range opts.Extensions {
		args = append(args, "-e", strings.TrimPrefix(extension, "."))
	}
	return append(args, "-o", outputPath)
}

// **Validates: Requirements 2.4, 3.1, 3.2, 3.4**
func TestFFufDefaultMatchCodesPreservation(t *testing.T) {
	for _, tt := range []struct {
		name        string
		statusCodes []int
	}{
		{name: "nil status codes", statusCodes: nil},
		{name: "empty status codes", statusCodes: []int{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opts := &FFufOptions{
				Threads:         20,
				Timeout:         10,
				AutoCalibration: false,
				StatusCodes:     tt.statusCodes,
			}
			args := buildFFufArgs("https://example.test/FUZZ", "wordlist.txt", "output.json", opts)

			if got := countFFufFlag(args, "-mc"); got != 1 {
				t.Fatalf("COUNT_FLAG(args, \"-mc\")=%d, want 1; args=%q", got, args)
			}
			if got, ok := valueAfterFFufFlag(args, "-mc"); !ok || got != ffufDefaultMatchCodesForExploration {
				t.Fatalf("VALUE_AFTER_FLAG(args, \"-mc\")=%q, present=%t, want %q; args=%q", got, ok, ffufDefaultMatchCodesForExploration, args)
			}
			if got := countFFufFlag(args, "-ac"); got != 0 {
				t.Fatalf("COUNT_FLAG(args, \"-ac\")=%d, want 0; args=%q", got, args)
			}
		})
	}
}

// **Validates: Requirements 2.4, 3.4**
func TestFFufUnrelatedArgumentsPreservation(t *testing.T) {
	const (
		baseURL     = "https://example.test/FUZZ"
		wordlist    = "wordlist.txt"
		outputPath  = "output.json"
		defaultCode = ffufDefaultMatchCodesForExploration
	)

	tests := []struct {
		name     string
		opts     FFufOptions
		expected []string
	}{
		{
			name: "empty optional values",
			opts: FFufOptions{Threads: 0, Timeout: 0},
			expected: []string{
				"-u", baseURL, "-w", wordlist, "-of", "json", "-t", "0", "-timeout", "0",
				"-mc", defaultCode, "-o", outputPath,
			},
		},
		{
			name: "single extension",
			opts: FFufOptions{Threads: 17, Timeout: 9, Extensions: []string{".php"}},
			expected: []string{
				"-u", baseURL, "-w", wordlist, "-of", "json", "-t", "17", "-timeout", "9",
				"-mc", defaultCode, "-e", "php", "-o", outputPath,
			},
		},
		{
			name: "combined conditional values preserve order",
			opts: FFufOptions{
				Threads:        31,
				Timeout:        12,
				FollowRedirect: true,
				Recursion:      true,
				RecursionDepth: 4,
				Rate:           73,
				Extensions:     []string{".php", "js", "..bak"},
			},
			expected: []string{
				"-u", baseURL, "-w", wordlist, "-of", "json", "-t", "31", "-timeout", "12",
				"-mc", defaultCode, "-r", "-recursion", "-recursion-depth", "4", "-rate", "73",
				"-e", "php", "-e", "js", "-e", ".bak", "-o", outputPath,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := buildFFufArgs(baseURL, wordlist, outputPath, &tt.opts)
			if !reflect.DeepEqual(args, tt.expected) {
				t.Fatalf("buildFFufArgs()=%q, want observed pre-fix args %q", args, tt.expected)
			}
		})
	}
}

// **Validates: Requirements 2.4, 3.1, 3.2, 3.4**
func TestFFufUnrelatedArgumentsPreservationProperty(t *testing.T) {
	property := func(generated ffufPreservationQuickInput) bool {
		statusCodes := []int(nil)
		if generated.UseCustomStatusCodes {
			statusCodes = []int{503, 201, 503}
		}
		opts := &FFufOptions{
			Threads:         23,
			Timeout:         11,
			FollowRedirect:  generated.FollowRedirect,
			Recursion:       generated.Recursion,
			RecursionDepth:  generated.RecursionDepth,
			Rate:            generated.Rate,
			Extensions:      generated.Extensions,
			AutoCalibration: generated.AutoCalibration,
			StatusCodes:     statusCodes,
		}

		const baseURL, wordlist, outputPath = "https://property.test/FUZZ", "property.txt", "property.json"
		args := buildFFufArgs(baseURL, wordlist, outputPath, opts)
		if countFFufFlag(args, "-mc") != 1 {
			return false
		}

		actualProjection := ffufUnrelatedArgsProjection(args)
		expectedProjection := expectedFFufUnrelatedArgs(baseURL, wordlist, outputPath, opts)
		return reflect.DeepEqual(actualProjection, expectedProjection)
	}

	if err := quick.Check(property, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatalf("FFUF unrelated argument preservation counterexample: %v", err)
	}
}
