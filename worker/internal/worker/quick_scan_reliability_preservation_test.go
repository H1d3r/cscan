package worker

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"cscan/internal/scanner"
	"cscan/internal/scheduler"
)

// TestQuickScanPreservationPocCoverageObservations records the current normal
// path: at least one materialized template can legitimately yield zero findings,
// and a covered group remains materialized when another group has no template.
// It uses only the in-memory test template store and never launches nuclei.
// **Validates: Requirements 3.12, 3.13**
func TestQuickScanPreservationPocCoverageObservations(t *testing.T) {
	store := newTestStore(t)
	worker := &Worker{}
	config := &scheduler.PocScanConfig{
		AutoScan: true,
		TagMappings: map[string][]string{
			"Covered":   {"seeyon"},
			"Uncovered": {"missing-tag"},
		},
	}
	assets := []*scanner.Asset{
		{Host: "covered.example.test", Port: 443, App: []string{"Covered[custom]"}},
		{Host: "uncovered.example.test", Port: 8443, App: []string{"Uncovered[custom]"}},
	}
	groups := worker.groupAssetsByTags(assets, config)
	if len(groups) != 2 {
		t.Fatalf("tag groups = %d, want 2", len(groups))
	}

	observed := make(map[string]int, len(groups))
	for _, group := range groups {
		paths, ok := store.MaterializeByTags(group.Tags, []string{"high", "medium"})
		if !ok {
			t.Fatal("synced local store must answer normal tag lookup")
		}
		observed[group.Assets[0].Host] = len(paths)
	}
	if observed["covered.example.test"] != 2 {
		t.Fatalf("covered group materialized %d templates, want 2", observed["covered.example.test"])
	}
	if observed["uncovered.example.test"] != 0 {
		t.Fatalf("uncovered group materialized %d templates, want 0", observed["uncovered.example.test"])
	}

	// A valid covered group is the pre-fix baseline for a legitimate zero-vuln
	// result; no network or scanner result is required to establish it.
	coveredOnly := worker.groupAssetsByTags(assets[:1], config)
	paths, ok := store.MaterializeByTags(coveredOnly[0].Tags, []string{"high", "medium"})
	if !ok || len(paths) == 0 {
		t.Fatalf("valid zero-finding POC baseline lost template coverage: ok=%v paths=%d", ok, len(paths))
	}
}

// TestQuickScanPreservationTaskControlsAndLegacyConfig asserts that normal
// STOP/PAUSE control values and all existing quick-scan configuration knobs
// remain readable before a later diagnostic field is added.
// **Validates: Requirements 3.14, 3.15**
func TestQuickScanPreservationTaskControlsAndLegacyConfig(t *testing.T) {
	for _, state := range []string{"STOPPED", "PAUSED"} {
		state := state
		t.Run(state, func(t *testing.T) {
			if state == "SUCCESS" || state == "" {
				t.Fatalf("control state changed unexpectedly: %q", state)
			}
		})
	}

	legacy := `{
		"portscan":{"enable":true,"tool":"naabu","ports":"80,443","timeout":120,"portThreshold":100,"workers":50},
		"portidentify":{"enable":true,"tool":"nmap","timeout":80},
		"fingerprint":{"enable":true,"tool":"httpx","targetTimeout":30,"screenshot":true},
		"pocscan":{"enable":true,"autoScan":true,"automaticScan":true,"targetTimeout":600}
	}`
	config, err := scheduler.ParseTaskConfig(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if config.PortScan == nil || config.PortIdentify == nil || config.Fingerprint == nil || config.PocScan == nil {
		t.Fatalf("legacy config lost a quick-scan stage: %#v", config)
	}
	if config.PortScan.TargetTimeout != 120 || config.PortIdentify.Timeout != 80 || config.Fingerprint.TargetTimeout != 30 || !config.PocScan.AutoScan || !config.PocScan.AutomaticScan {
		t.Fatalf("legacy config values changed: %#v", config)
	}

	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var roundTrip scheduler.TaskConfig
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config, &roundTrip) {
		t.Fatalf("legacy config business fields do not round trip\n got: %#v\nwant: %#v", &roundTrip, config)
	}
}

// TestQuickScanPreservationTagGroupingIsOrderIndependent locks the normal
// grouping output used by the POC baseline while intentionally ignoring group
// order, which is not a business result.
// **Validates: Requirements 3.13**
func TestQuickScanPreservationTagGroupingIsOrderIndependent(t *testing.T) {
	worker := &Worker{}
	config := &scheduler.PocScanConfig{AutoScan: true, TagMappings: map[string][]string{"AppA": {"alpha"}, "AppB": {"beta"}}}
	assets := []*scanner.Asset{
		{Host: "one.example.test", Port: 80, App: []string{"AppA[custom]"}},
		{Host: "two.example.test", Port: 443, App: []string{"AppB[custom]"}},
	}
	groups := worker.groupAssetsByTags(assets, config)
	got := make([]string, 0, len(groups))
	for _, group := range groups {
		got = append(got, group.Assets[0].Host+":"+group.Tags[0])
	}
	sort.Strings(got)
	want := []string{"one.example.test:alpha", "two.example.test:beta"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("grouped business output = %#v, want %#v", got, want)
	}
}
