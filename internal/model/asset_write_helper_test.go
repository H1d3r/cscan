package model

import (
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

func setStr(m bson.M, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func setHas(m bson.M, key string) bool {
	_, ok := m[key]
	return ok
}

func TestBuildAssetUpdateDoc_NewAsset_IncludesSetOnInsert(t *testing.T) {
	asset := &Asset{
		Authority: "example.com:80",
		Host:      "example.com",
		Port:      80,
		Title:     "Hello",
	}
	opts := AssetWriteOptions{TaskId: "task-1"}
	update, _ := BuildAssetUpdateDoc(asset, nil, opts)

	setOnInsert, ok := update["$setOnInsert"].(bson.M)
	if !ok {
		t.Fatalf("expected $setOnInsert for new asset")
	}
	for _, key := range []string{"_id", "create_time", "first_seen_time", "first_seen_task_id", "new"} {
		if !setHas(setOnInsert, key) {
			t.Errorf("$setOnInsert missing key %q", key)
		}
	}
	if setOnInsert["first_seen_task_id"] != "task-1" {
		t.Errorf("first_seen_task_id mismatch: %v", setOnInsert["first_seen_task_id"])
	}
}

func TestBuildAssetUpdateDoc_EmptyValueDoesNotOverwrite(t *testing.T) {
	existing := &Asset{
		Authority:  "example.com:80",
		Host:       "example.com",
		Port:       80,
		Title:      "OldTitle",
		HttpHeader: "X-Old: 1",
	}
	asset := &Asset{
		Authority: "example.com:80",
		Host:      "example.com",
		Port:      80,
		Title:     "", // empty — must not overwrite
	}
	opts := AssetWriteOptions{TaskId: "task-2"}
	update, _ := BuildAssetUpdateDoc(asset, existing, opts)
	setFields := update["$set"].(bson.M)
	if setHas(setFields, "title") {
		t.Errorf("title must be omitted when new value is empty")
	}
	if setHas(setFields, "header") {
		t.Errorf("header must be omitted when new value is empty")
	}
	// update_time should not advance on no-op
	if setHas(setFields, "update_time") {
		t.Errorf("update_time must not advance on no-op write; set keys=%v", keysOf(setFields))
	}
}

func keysOf(m bson.M) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestBuildAssetUpdateDoc_ManualCanClearMemo(t *testing.T) {
	existing := &Asset{
		Authority: "example.com:80",
		Host:      "example.com",
		Port:      80,
		Memo:      "旧备注",
	}
	asset := &Asset{
		Authority: "example.com:80",
		Host:      "example.com",
		Port:      80,
		Memo:      "", // clear
	}
	opts := AssetWriteOptions{IsManual: true, AllowClearUserFields: true}
	update, _ := BuildAssetUpdateDoc(asset, existing, opts)
	setFields := update["$set"].(bson.M)
	if !setHas(setFields, "memo") {
		t.Errorf("manual write must include memo when AllowClearUserFields=true")
	}
	if got := setStr(setFields, "memo"); got != "" {
		t.Errorf("memo must be empty (clear), got %q", got)
	}
	// changes should record the memo clear
	var hasMemoChange bool
	for _, c := range updateChangesLookup(update) {
		_ = c
	}
	if changes := diffAssetChanges(existing, asset, opts); len(changes) == 0 {
		t.Errorf("expected memo change to be detected")
	} else {
		hasMemoChange = false
		for _, c := range changes {
			if c.Field == "memo" {
				hasMemoChange = true
			}
		}
		if !hasMemoChange {
			t.Errorf("expected memo change in diff")
		}
	}
}

func TestBuildAssetUpdateDoc_StateFields_GatedByIsDifferentTask(t *testing.T) {
	existing := &Asset{
		Authority: "example.com:80",
		Host:      "example.com",
		Port:      80,
		TaskId:    "old-task",
		Title:     "Old",
	}
	asset := &Asset{
		Authority: "example.com:80",
		Host:      "example.com",
		Port:      80,
		TaskId:    "new-task",
		Title:     "New",
	}

	// Same task — should NOT mutate state fields
	update, _ := BuildAssetUpdateDoc(asset, existing, AssetWriteOptions{TaskId: "new-task", IsDifferentTask: false})
	if setFields, ok := update["$set"].(bson.M); ok {
		for _, key := range []string{"update", "new", "last_task_id", "last_status_change_time"} {
			if setHas(setFields, key) {
				t.Errorf("same-task write must not set %q", key)
			}
		}
	}

	// Different task — should advance state fields
	update2, _ := BuildAssetUpdateDoc(asset, existing, AssetWriteOptions{TaskId: "new-task", IsDifferentTask: true})
	setFields2 := update2["$set"].(bson.M)
	for _, key := range []string{"update", "new", "last_task_id", "last_status_change_time"} {
		if !setHas(setFields2, key) {
			t.Errorf("cross-task write must set %q", key)
		}
	}
	if setFields2["last_task_id"] != "old-task" {
		t.Errorf("last_task_id must be existing.TaskId, got %v", setFields2["last_task_id"])
	}
}

func TestDiffAssetChanges_CoversLargeFields(t *testing.T) {
	existing := &Asset{
		Authority:  "example.com:80",
		HttpHeader: "X-Old: 1",
		HttpBody:   "<html>old</html>",
		Screenshot: "old.png",
		Cert:       "oldcert",
	}
	updated := &Asset{
		Authority:  "example.com:80",
		HttpHeader: "X-New: 2",
		HttpBody:   "<html>new</html>",
		Screenshot: "new.png",
		Cert:       "newcert",
	}
	changes := DiffAssetChanges(existing, updated, AssetWriteOptions{})
	fields := map[string]bool{}
	for _, c := range changes {
		fields[c.Field] = true
	}
	for _, want := range []string{"header", "body", "screenshot", "cert"} {
		if !fields[want] {
			t.Errorf("diff missing field %q; got changes=%v", want, changes)
		}
	}
}

func TestDiffAssetChanges_NoneWhenEqual(t *testing.T) {
	asset := &Asset{
		Authority: "example.com:80",
		Title:    "Same",
		App:      []string{"nginx"},
	}
	changes := DiffAssetChanges(asset, asset, AssetWriteOptions{})
	if len(changes) != 0 {
		t.Errorf("expected no changes for identical assets, got %d", len(changes))
	}
}

func TestSortedJoin_OrderIndependent(t *testing.T) {
	a := sortedJoin([]string{"b", "a", "c"})
	b := sortedJoin([]string{"c", "a", "b"})
	if a != b {
		t.Errorf("sortedJoin not order-independent: a=%q b=%q", a, b)
	}
	if a != "a, b, c" {
		t.Errorf("sortedJoin wrong: got %q", a)
	}
}

func TestTruncateForChange(t *testing.T) {
	if got := truncateForChange("hello", 0); got != "hello" {
		t.Errorf("maxLen=0 should return full string, got %q", got)
	}
	if got := truncateForChange("hello", 10); got != "hello" {
		t.Errorf("short string should be unchanged, got %q", got)
	}
	long := strings.Repeat("a", 300)
	if got := truncateForChange(long, 50); len(got) != 53 {
		t.Errorf("expected len 53 (50 + ellipsis), got %d", len(got))
	}
}

func TestIpChanged(t *testing.T) {
	a := IP{IpV4: []IPV4{{IPName: "1.1.1.1"}}}
	b := IP{IpV4: []IPV4{{IPName: "2.2.2.2"}}}
	if !ipChanged(a, b) {
		t.Error("expected ipChanged true for different IPv4")
	}
	if ipChanged(a, a) {
		t.Error("expected ipChanged false for identical IPs")
	}
}

func TestBuildAssetUpdateDoc_LabelsUseAddToSet(t *testing.T) {
	existing := &Asset{
		Authority: "example.com:80",
		Host:      "example.com",
		Port:      80,
		Labels:    []string{"existing"},
	}
	asset := &Asset{
		Authority: "example.com:80",
		Host:      "example.com",
		Port:      80,
		Labels:    []string{"new1", "new2"},
	}
	update, _ := BuildAssetUpdateDoc(asset, existing, AssetWriteOptions{})
	addToSet, ok := update["$addToSet"].(bson.M)
	if !ok {
		t.Fatalf("expected $addToSet when Labels non-empty")
	}
	labelsEach, ok := addToSet["labels"].(bson.M)
	if !ok {
		t.Fatalf("expected labels with $each")
	}
	if _, ok := labelsEach["$each"]; !ok {
		t.Errorf("missing $each on labels")
	}
	// $set must NOT contain labels (would overwrite)
	if setHas(update["$set"].(bson.M), "labels") {
		t.Errorf("labels must NOT be in $set (would overwrite)")
	}
}

func TestBuildAssetUpdateDoc_HasChangeAdvancesUpdateTime(t *testing.T) {
	existing := &Asset{
		Authority: "example.com:80",
		Host:      "example.com",
		Port:      80,
		Title:     "Old",
	}
	updated := &Asset{
		Authority: "example.com:80",
		Host:      "example.com",
		Port:      80,
		Title:     "New",
	}
	update, _ := BuildAssetUpdateDoc(updated, existing, AssetWriteOptions{})
	setFields := update["$set"].(bson.M)
	if !setHas(setFields, "update_time") {
		t.Errorf("update_time must advance when there is a real change")
	}
	// Should be a recent time (within the last 5 seconds)
	now := time.Now()
	ut, ok := setFields["update_time"].(time.Time)
	if !ok {
		t.Errorf("update_time not time.Time: %T", setFields["update_time"])
		return
	}
	if now.Sub(ut) > 5*time.Second {
		t.Errorf("update_time too old: %v", ut)
	}
}

// updateChangesLookup is a no-op helper kept for future use; returns the
// FieldChange slice decoded from update's $set / $setOnInsert if available.
func updateChangesLookup(update bson.M) []FieldChange {
	// BuildAssetUpdateDoc returns changes via the second return value;
	// this helper exists to keep tests future-proof if needed.
	return nil
}

func TestNormalizeAppKey(t *testing.T) {
	cases := map[string]string{
		"Nginx":                                "nginx",
		"Nginx[httpx]":                         "nginx",
		"nginx [httpx+wappalyzer]":             "nginx",
		"Nginx:1.18.0[httpx]":                  "nginx",
		"Kibana[httpx+wappalyzer+custom(abc)]": "kibana",
		"  Apache  ":                           "apache",
		"":                                     "",
		"[httpx]":                              "",
	}
	for in, want := range cases {
		if got := NormalizeAppKey(in); got != want {
			t.Errorf("NormalizeAppKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMergeAppsDedup(t *testing.T) {
	// 变体收敛：同技术不同来源后缀只保留信息最全的一条
	merged := MergeAppsDedup(
		[]string{"Nginx[httpx]", "Apache[httpx]"},
		[]string{"Nginx[httpx+wappalyzer+custom(64a1f2)]", "Redis"},
	)
	want := []string{"Nginx[httpx+wappalyzer+custom(64a1f2)]", "Apache[httpx]", "Redis"}
	if sortedJoin(merged) != sortedJoin(want) {
		t.Errorf("merged = %v, want %v", merged, want)
	}

	// 版本号与大小写变体折叠
	merged = MergeAppsDedup([]string{"Nginx:1.18.0[httpx]"}, []string{"nginx"})
	if len(merged) != 1 || merged[0] != "Nginx:1.18.0[httpx]" {
		t.Errorf("version/case variants not collapsed: %v", merged)
	}

	// 完全相同的条目不产生新增
	merged = MergeAppsDedup([]string{"Nginx[httpx]"}, []string{"Nginx[httpx]"})
	if len(merged) != 1 {
		t.Errorf("identical entries duplicated: %v", merged)
	}

	// 空输入
	if got := MergeAppsDedup(nil, nil); got != nil {
		t.Errorf("nil inputs should return nil, got %v", got)
	}
}

func TestBuildAssetUpdateDoc_AppCollapsedNotAccumulated(t *testing.T) {
	existing := &Asset{
		Authority: "example.com:80",
		Host:      "example.com",
		Port:      80,
		App:       []string{"Nginx[httpx]", "Apache[httpx]"},
	}
	// 下一轮流式写入只带来了更长后缀的同技术变体与一个新技术
	incoming := &Asset{
		Authority: "example.com:80",
		Host:      "example.com",
		Port:      80,
		App:       []string{"Nginx[httpx+wappalyzer+custom(64a1f2)]", "Redis[httpx]"},
	}
	update, changes := BuildAssetUpdateDoc(incoming, existing, AssetWriteOptions{TaskId: "task-2"})

	setFields := update["$set"].(bson.M)
	apps, ok := setFields["app"]
	if !ok {
		t.Fatalf("app must be written via $set when merge result differs, set keys=%v", keysOf(setFields))
	}
	merged, ok := apps.([]string)
	if !ok {
		t.Fatalf("app in $set is not []string: %T", apps)
	}
	want := []string{"Nginx[httpx+wappalyzer+custom(64a1f2)]", "Apache[httpx]", "Redis[httpx]"}
	if sortedJoin(merged) != sortedJoin(want) {
		t.Errorf("app = %v, want %v", merged, want)
	}
	// app 不再走 $addToSet（旧变体必须被收敛而不是追加）
	if addSet, ok := update["$addToSet"].(bson.M); ok {
		if setHas(addSet, "app") {
			t.Errorf("app must not use $addToSet anymore")
		}
	}
	var hasAppChange bool
	for _, c := range changes {
		if c.Field == "app" {
			hasAppChange = true
		}
	}
	if !hasAppChange {
		t.Errorf("app change must be reported when variants are collapsed")
	}
}

func TestBuildAssetUpdateDoc_AppNoChangeWhenCollapsedEqualsExisting(t *testing.T) {
	existing := &Asset{
		Authority: "example.com:80",
		Host:      "example.com",
		Port:      80,
		App:       []string{"Nginx[httpx+wappalyzer]", "Apache[httpx]"},
	}
	// 本轮检测结果的来源后缀信息少于库内已有条目：合并后与库内一致，不应写 app、不应报变更
	incoming := &Asset{
		Authority: "example.com:80",
		Host:      "example.com",
		Port:      80,
		App:       []string{"Nginx[httpx]"},
	}
	update, changes := BuildAssetUpdateDoc(incoming, existing, AssetWriteOptions{})
	setFields := update["$set"].(bson.M)
	if setHas(setFields, "app") {
		t.Errorf("app must not be written when merge result equals existing")
	}
	for _, c := range changes {
		if c.Field == "app" {
			t.Errorf("app change must not be reported when nothing actually changes")
		}
	}
	if setHas(setFields, "update_time") {
		t.Errorf("no-op write must not advance update_time; set keys=%v", keysOf(setFields))
	}
}

func TestBuildAssetUpdateDoc_AppEmptyIncomingKeepsExisting(t *testing.T) {
	existing := &Asset{
		Authority: "example.com:80",
		Host:      "example.com",
		Port:      80,
		App:       []string{"Nginx[httpx]"},
	}
	incoming := &Asset{
		Authority: "example.com:80",
		Host:      "example.com",
		Port:      80,
	}
	update, _ := BuildAssetUpdateDoc(incoming, existing, AssetWriteOptions{})
	setFields := update["$set"].(bson.M)
	if setHas(setFields, "app") {
		t.Errorf("empty App must be omitted (omit-if-empty preserves existing apps)")
	}
}
