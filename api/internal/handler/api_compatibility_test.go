package handler

import (
	"encoding/json"
	"reflect"
	"testing"

	"cscan/api/internal/types"
)

// TestResponseContracts verifies the JSON fields that clients depend on. Route
// registration is intentionally tested through handler tests instead of a
// second, hand-maintained endpoint list that can drift from routes.go.
func TestResponseContracts(t *testing.T) {
	cases := []struct {
		name   string
		value  any
		fields []string
	}{
		{"base", types.BaseResp{}, []string{"code", "msg"}},
		{"login", types.LoginResp{}, []string{"code", "msg", "token", "userId", "username", "role"}},
		{"asset list", types.AssetListResp{}, []string{"code", "msg", "total", "list"}},
		{"vulnerability list", types.VulListResp{}, []string{"code", "msg", "total", "list"}},
		{"worker list", types.WorkerListResp{}, []string{"code", "msg", "list"}},
		{"task list", types.MainTaskListResp{}, []string{"code", "msg", "total", "list"}},
		{"asset", types.Asset{}, []string{"id", "authority", "host", "port", "service", "title", "app", "createTime"}},
		{"vulnerability", types.Vul{}, []string{"id", "authority", "url", "pocFile", "severity", "result", "createTime"}},
		{"worker", types.Worker{}, []string{"name", "ip", "cpuLoad", "memUsed", "status", "updateTime"}},
		{"task", types.MainTask{}, []string{"id", "taskId", "name", "target", "status", "progress", "createTime"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			typ := reflect.TypeOf(tc.value)
			for _, field := range tc.fields {
				if !hasJSONTag(typ, field) {
					t.Errorf("%s is missing required JSON field %q", typ.Name(), field)
				}
			}
		})
	}
}

func TestResponseSerializationKeepsRequiredFields(t *testing.T) {
	asset := types.Asset{
		Id:        "asset-1",
		Authority: "example.com:443",
		Host:      "example.com",
		Port:      443,
		Service:   "https",
		Title:     "Example",
		App:       []string{},
	}

	data, err := json.Marshal(asset)
	if err != nil {
		t.Fatalf("marshal asset: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal asset: %v", err)
	}
	for _, field := range []string{"id", "authority", "host", "port", "service", "title"} {
		if _, ok := decoded[field]; !ok {
			t.Errorf("serialized asset is missing required field %q", field)
		}
	}
}

func TestOptionalRequestFieldsUseZeroValues(t *testing.T) {
	pageReq := types.PageReq{}
	if pageReq.Page != 0 {
		t.Errorf("PageReq.Page = %d, want zero value", pageReq.Page)
	}

	assetReq := types.AssetListReq{}
	if assetReq.Query != "" || assetReq.Host != "" || assetReq.OnlyNew {
		t.Errorf("AssetListReq optional fields do not use zero values: %+v", assetReq)
	}
}

func hasJSONTag(typ reflect.Type, tagName string) bool {
	for i := 0; i < typ.NumField(); i++ {
		jsonTag := typ.Field(i).Tag.Get("json")
		if jsonTag == tagName || len(jsonTag) > len(tagName) && jsonTag[:len(tagName)+1] == tagName+"," {
			return true
		}
	}
	return false
}
