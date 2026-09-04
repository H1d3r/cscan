package scanner

import (
	"testing"
	"time"
)

func flagValue(args []string, flag string) (string, int) {
	count := 0
	value := ""
	for i, arg := range args {
		if arg != flag {
			continue
		}
		count++
		if i+1 < len(args) {
			value = args[i+1]
		}
	}
	return value, count
}

func requireFlagValue(t *testing.T, args []string, flag, want string) {
	t.Helper()
	got, count := flagValue(args, flag)
	if count != 1 {
		t.Fatalf("flag %s appears %d times, want 1; args=%q", flag, count, args)
	}
	if got != want {
		t.Fatalf("value after %s=%q, want %q; args=%q", flag, got, want, args)
	}
}

func TestEffectiveNaabuProcessConcurrency(t *testing.T) {
	tests := []struct {
		name        string
		workers     int
		targetCount int
		want        int
	}{
		{name: "no targets", workers: 50, targetCount: 0, want: 0},
		{name: "negative target count", workers: 50, targetCount: -1, want: 0},
		{name: "default workers", workers: 0, targetCount: 3, want: 1},
		{name: "negative workers", workers: -1, targetCount: 3, want: 1},
		{name: "limited by target count", workers: 4, targetCount: 2, want: 2},
		{name: "limited by process cap", workers: 50, targetCount: 20, want: 5},
		{name: "configured workers", workers: 3, targetCount: 20, want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EffectiveNaabuProcessConcurrency(tt.workers, tt.targetCount); got != tt.want {
				t.Fatalf("EffectiveNaabuProcessConcurrency(%d, %d)=%d, want %d", tt.workers, tt.targetCount, got, tt.want)
			}
		})
	}
}

func TestCountNaabuProcessTargetsExpandsAndDeduplicates(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   int
	}{
		{name: "empty", target: "", want: 0},
		{name: "duplicate host", target: "example.com\nexample.com", want: 1},
		{name: "cidr", target: "192.0.2.0/30", want: 2},
		{name: "ip range", target: "198.51.100.10-198.51.100.12", want: 3},
		{
			name:   "mixed targets",
			target: "192.0.2.0/30\n192.0.2.1\n198.51.100.10-198.51.100.12\nhttps://example.com",
			want:   6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CountNaabuProcessTargets(tt.target); got != tt.want {
				t.Fatalf("CountNaabuProcessTargets(%q)=%d, want %d", tt.target, got, tt.want)
			}
		})
	}
}

func TestBuildNaabuArgsSeparatesTargetAndProbeTimeouts(t *testing.T) {
	for _, scanType := range []string{"s", "c"} {
		t.Run(scanType, func(t *testing.T) {
			opts := &NaabuOptions{
				Ports:             "80,443",
				Rate:              3000,
				TargetTimeout:     75,
				ProbeTimeoutMs:    1250,
				ScanType:          scanType,
				PortThreshold:     100,
				SkipHostDiscovery: true,
				ExcludeCDN:        true,
				ExcludeHosts:      "192.0.2.1",
				Retries:           2,
				WarmUpTime:        1,
				Workers:           37,
				Verify:            true,
			}

			args := buildNaabuArgs("example.com", "80,443", "/tmp/naabu-output.json", opts)
			requireFlagValue(t, args, "-timeout", "1250")
			requireFlagValue(t, args, "-s", scanType)
			requireFlagValue(t, args, "-c", "37")
			requireFlagValue(t, args, "-o", "/tmp/naabu-output.json")

			if got := opts.targetTimeoutDuration(); got != 75*time.Second {
				t.Fatalf("target timeout duration=%s, want %s", got, 75*time.Second)
			}
		})
	}
}

func TestBuildNaabuArgsPassesTopPortsThrough(t *testing.T) {
	for _, tt := range []struct {
		ports string
		want  string
	}{
		{ports: "top100", want: "100"},
		{ports: "top1000", want: "1000"},
	} {
		t.Run(tt.ports, func(t *testing.T) {
			opts := &NaabuOptions{
				Ports:          tt.ports,
				Rate:           1000,
				TargetTimeout:  10,
				ProbeTimeoutMs: 1000,
				ScanType:       "c",
				Workers:        1,
			}
			args := buildNaabuArgs("host1", "", "/tmp/naabu-output.json", opts)
			requireFlagValue(t, args, "-tp", tt.want)
			if _, count := flagValue(args, "-p"); count != 0 {
				t.Fatalf("top ports unexpectedly emitted -p; args=%q", args)
			}
		})
	}
}
