package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// newTestStore 构建带测试索引的本地模板库（不经 Mongo 同步，直接注入条目与文件）
func newTestStore(t *testing.T) *TemplateStore {
	t.Helper()
	s := NewTemplateStore(t.TempDir(), nil)

	// 三个模板：启用的 nuclei、禁用的 nuclei、启用的自定义POC
	contents := map[string]string{
		"n:cve-2024-0001": "id: cve-2024-0001\ninfo:\n  name: tpl-a\n",
		"n:cve-2024-0002": "id: cve-2024-0002\ninfo:\n  name: tpl-b\n",
		"c:poc-1":         "id: custom-poc-1\ninfo:\n  name: poc-a\n",
	}
	entries := map[string]*TemplateEntry{
		"n:cve-2024-0001": {Hash: hashOfContent(contents["n:cve-2024-0001"]), Enabled: true, Severity: "high", Tags: []string{"seeyon", "rce"}},
		"n:cve-2024-0002": {Hash: hashOfContent(contents["n:cve-2024-0002"]), Enabled: false, Severity: "critical", Tags: []string{"apache"}},
		"c:poc-1":         {Hash: hashOfContent(contents["c:poc-1"]), Enabled: true, Severity: "medium", Tags: []string{"seeyon"}},
	}
	written := 0
	for _, c := range contents {
		if _, err := s.ensureFile(c, &written); err != nil {
			t.Fatalf("ensureFile: %v", err)
		}
	}
	s.entries = entries
	s.synced = true
	return s
}

func TestTemplateStoreMaterializeIDs(t *testing.T) {
	s := newTestStore(t)

	paths, missedN, missedC := s.MaterializeIDs([]string{"cve-2024-0001", "cve-2024-0002", "cve-9999"}, []string{"poc-1", "507f1f77bcf86cd799439011"})
	// 启用模板命中 2 个；禁用模板（cve-2024-0002）静默跳过；索引未命中的进入 missed 回退列表
	if len(paths) != 2 {
		t.Fatalf("expected 2 resolved paths, got %d", len(paths))
	}
	if len(missedN) != 1 || missedN[0] != "cve-9999" {
		t.Fatalf("expected missed nuclei [cve-9999], got %v", missedN)
	}
	if len(missedC) != 1 || missedC[0] != "507f1f77bcf86cd799439011" {
		t.Fatalf("expected missed custom [507f...], got %v", missedC)
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("resolved path missing on disk: %v", err)
		}
	}
}

func TestTemplateStoreMaterializeByTags(t *testing.T) {
	s := newTestStore(t)

	// 标签命中应同时覆盖 nuclei 与自定义POC；禁用项与 severity 不符项排除
	paths, ok := s.MaterializeByTags([]string{"seeyon"}, []string{"high", "medium"})
	if !ok || len(paths) != 2 {
		t.Fatalf("expected 2 paths (nuclei+custom) for tag seeyon, got ok=%v len=%d", ok, len(paths))
	}

	paths, ok = s.MaterializeByTags([]string{"seeyon"}, []string{"critical"})
	if !ok || len(paths) != 0 {
		t.Fatalf("severity filter should exclude all seeyon entries, got %d", len(paths))
	}

	// 无标签条件时不做 tags 过滤，仅按 severity
	paths, ok = s.MaterializeByTags(nil, []string{"critical"})
	if !ok || len(paths) != 0 {
		t.Fatalf("disabled apache template must be excluded, got %d", len(paths))
	}

	// 库未同步时必须返回 ok=false 让调用方回退
	s.synced = false
	if _, ok := s.MaterializeByTags([]string{"seeyon"}, nil); ok {
		t.Fatal("unsynced store must not answer tag queries")
	}
}

func TestTemplateStoreCustomPocsOnly(t *testing.T) {
	s := newTestStore(t)

	paths, ok := s.MaterializeCustomPocs(nil)
	if !ok || len(paths) != 1 {
		t.Fatalf("expected 1 enabled custom poc, got %d", len(paths))
	}
	if filepath.Base(paths[0]) != fileNameOfHash(hashOfContent("id: custom-poc-1\ninfo:\n  name: poc-a\n")) {
		t.Fatalf("unexpected file: %s", paths[0])
	}
}

func TestTemplateStoreCleanupOrphansAndIndexRoundtrip(t *testing.T) {
	s := newTestStore(t)

	// 制造孤儿文件
	orphan := filepath.Join(s.baseDir, "deadbeefdeadbeef.yaml")
	if err := os.WriteFile(orphan, []byte("orphan"), 0644); err != nil {
		t.Fatal(err)
	}
	if removed := s.cleanupOrphans(s.entries); removed != 1 {
		t.Fatalf("expected 1 orphan removed, got %d", removed)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("orphan file should be removed")
	}

	// 索引落盘 + 重启加载 roundtrip
	s.persistIndex(TemplateFingerprint{NucleiTotal: 3, NucleiEnabled: 2})
	s2 := NewTemplateStore(s.baseDir, nil)
	if !s2.loadedFromDisk || len(s2.entries) != 3 || s2.fp.NucleiTotal != 3 {
		t.Fatalf("index roundtrip failed: loaded=%v entries=%d", s2.loadedFromDisk, len(s2.entries))
	}

	// 指纹比较：数值与秒级时间一致即相等（不受 time.Time loc 指针影响）
	if !(TemplateFingerprint{NucleiTotal: 3}).equal(TemplateFingerprint{NucleiTotal: 3}) {
		t.Fatal("identical fingerprints must be equal")
	}
}

func validTemplate(id string) string {
	return "id: " + id + "\ninfo:\n  name: deterministic fixture\n  author: test\n  severity: info\nhttp:\n  - method: GET\n"
}

func TestTemplateLoadResultLocalHit(t *testing.T) {
	s := newTestStore(t)
	result, missedN, missedC := s.materializeIDsResult([]string{"cve-2024-0001"}, []string{"poc-1"})
	if result.Outcome != TemplateLoadLoaded || result.Source != "local_store" {
		t.Fatalf("unexpected local outcome: %+v", result)
	}
	if result.Requested != 2 || result.Loaded != 2 || result.Invalid != 0 {
		t.Fatalf("unexpected local counts: %+v", result)
	}
	if len(missedN) != 0 || len(missedC) != 0 || len(result.FileRefs) != 2 {
		t.Fatalf("local hit should not fall back: result=%+v missedN=%v missedC=%v", result, missedN, missedC)
	}
}

func TestTemplateLoadResultMongoFallback(t *testing.T) {
	s := newTestStore(t)
	fallbackCalls := 0
	result, err := resolveTemplateIDsWithFallback(t.Context(), s, []string{"cve-2024-0001", "mongo-only"}, nil,
		func(_ context.Context, req *TemplatesReq) (TemplateLoadResult, error) {
			fallbackCalls++
			if len(req.NucleiTemplateIds) != 1 || req.NucleiTemplateIds[0] != "mongo-only" {
				t.Fatalf("fallback received wrong IDs: %+v", req)
			}
			content := validTemplate("mongo-only")
			return TemplateLoadResult{
				Contents: []string{content}, Requested: 1, Loaded: 1,
				Source: "mongo", Outcome: TemplateLoadLoaded,
			}, nil
		})
	if err != nil {
		t.Fatalf("unexpected fallback error: %v", err)
	}
	if fallbackCalls != 1 || result.Outcome != TemplateLoadLoaded || result.Source != "mixed" || result.Loaded != 2 {
		t.Fatalf("unexpected mixed fallback result: calls=%d result=%+v", fallbackCalls, result)
	}
	if len(result.Contents) != 1 || len(result.FileRefs) != 1 || len(result.MissingIDs) != 0 {
		t.Fatalf("fallback should preserve both validated input forms: %+v", result)
	}
}

func TestTemplateLoadResultNoMatchDoesNotFallback(t *testing.T) {
	s := newTestStore(t)
	called := false
	result, err := resolveTemplateTagsWithFallback(t.Context(), s, []string{"does-not-exist"}, nil, false,
		func(context.Context, *TemplatesReq) (TemplateLoadResult, error) {
			called = true
			return TemplateLoadResult{}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if called || result.Outcome != TemplateLoadNoMatch || result.ReasonCode != "templates_no_match" {
		t.Fatalf("completed zero-match query must remain no_match without fallback: called=%v result=%+v", called, result)
	}
}

func TestTemplateLoadResultSeverityAndDisabledFiltering(t *testing.T) {
	s := newTestStore(t)

	severity := s.materializeByTagsResult([]string{"seeyon"}, []string{"critical"}, false)
	if severity.Outcome != TemplateLoadFiltered || severity.Requested != 2 || severity.Loaded != 0 {
		t.Fatalf("severity filtering not diagnosed: %+v", severity)
	}

	disabled, _, _ := s.materializeIDsResult([]string{"cve-2024-0002"}, nil)
	if disabled.Outcome != TemplateLoadFiltered || disabled.Requested != 1 || disabled.Loaded != 0 {
		t.Fatalf("disabled filtering not diagnosed: %+v", disabled)
	}
}

func TestTemplateLoadResultDBErrorIsReturned(t *testing.T) {
	expected := errors.New("deterministic database failure")
	result, err := resolveTemplateTagsWithFallback(t.Context(), nil, []string{"seeyon"}, nil, false,
		func(context.Context, *TemplatesReq) (TemplateLoadResult, error) {
			return TemplateLoadResult{Source: "mongo", Outcome: TemplateLoadDBError, ReasonCode: "mongo_nuclei_query_failed"}, expected
		})
	if !errors.Is(err, expected) {
		t.Fatalf("database error must be returned, got %v", err)
	}
	if result.Outcome != TemplateLoadDBError || result.ReasonCode != "mongo_nuclei_query_failed" {
		t.Fatalf("database error diagnostic lost: %+v", result)
	}
}

func TestTemplateLoadResultInvalidContent(t *testing.T) {
	valid, invalid := validateTemplateContents([]string{validTemplate("valid"), "id: [broken"})
	if len(valid) != 1 || invalid != 1 {
		t.Fatalf("content validation counts are wrong: valid=%d invalid=%d", len(valid), invalid)
	}

	result := TemplateLoadResult{Contents: valid, Requested: 2, Loaded: len(valid), Invalid: invalid, Source: "mongo"}
	classifyTemplateLoadResult(&result, 0, false)
	if result.Outcome != TemplateLoadInvalidContent || result.ReasonCode != "template_content_invalid" {
		t.Fatalf("invalid content must be explicit even when valid templates remain: %+v", result)
	}
}

func TestTemplateLoadResultMissingFile(t *testing.T) {
	s := newTestStore(t)
	entry := s.entries["n:cve-2024-0001"]
	if err := os.Remove(filepath.Join(s.baseDir, fileNameOfHash(entry.Hash))); err != nil {
		t.Fatal(err)
	}

	result, missedN, _ := s.materializeIDsResult([]string{"cve-2024-0001"}, nil)
	if result.Outcome != TemplateLoadInvalidContent || result.Invalid != 1 || result.Loaded != 0 {
		t.Fatalf("missing file must be invalid_content: %+v", result)
	}
	if len(missedN) != 1 || len(result.MissingIDs) != 1 || result.MissingIDs[0] != "cve-2024-0001" {
		t.Fatalf("missing file ID not reported: result=%+v missed=%v", result, missedN)
	}
}

func TestTemplateLoadMissingIDsAreBounded(t *testing.T) {
	var ids []string
	for i := 0; i < maxTemplateMissingIDs+10; i++ {
		ids = appendBoundedMissingID(ids, fmt.Sprintf("missing-%d", i))
	}
	if len(ids) != maxTemplateMissingIDs {
		t.Fatalf("missing IDs must be bounded to %d, got %d", maxTemplateMissingIDs, len(ids))
	}
}
