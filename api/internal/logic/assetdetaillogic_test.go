package logic

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/internal/model"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func newTestSvcCtxDB(t *testing.T) (*svc.ServiceContext, func()) {
	t.Helper()
	uri := os.Getenv("CSCAN_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("CSCAN_TEST_MONGO_URI not set, skip asset detail DB test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect MongoDB: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		t.Fatalf("ping MongoDB: %v", err)
	}

	db := client.Database("cscan_test_asset_detail_" + strings.ReplaceAll(t.Name(), "/", "_"))
	svcCtx := &svc.ServiceContext{MongoDB: db}
	cleanup := func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_ = db.Drop(cleanupCtx)
		_ = client.Disconnect(cleanupCtx)
	}
	return svcCtx, cleanup
}

// TestAssetDetail_FullFields 验证新增的按需详情接口返回完整资产（含 body/header/banner 大字段），
// 与清单列表投影（排除这些字段）形成互补。
func TestAssetDetail_FullFields(t *testing.T) {
	svcCtx, cleanup := newTestSvcCtxDB(t)
	defer cleanup()
	ctx := context.Background()

	assetModel := svcCtx.GetAssetModel()
	id := primitive.NewObjectID()
	doc := &model.Asset{
		Id:         id,
		Authority:  "example.com:443",
		Host:       "example.com",
		Port:       443,
		Title:      "Example",
		HttpBody:   "<html>full body</html>",
		HttpHeader: "HTTP/1.1 200 OK",
		Banner:     "nginx",
		Screenshot: "shot.png",
		UpdateTime: time.Now(),
		CreateTime: time.Now(),
	}
	if err := assetModel.Insert(ctx, doc); err != nil {
		t.Fatalf("插入资产失败: %v", err)
	}

	l := NewAssetDetailLogic(ctx, svcCtx)
	resp, err := l.AssetDetail(&types.AssetDetailReq{Id: id.Hex()})
	if err != nil {
		t.Fatalf("AssetDetail 失败: %v", err)
	}
	if resp.Code != 0 {
		t.Fatalf("期望 Code=0，实际 %d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data.Id != id.Hex() {
		t.Errorf("期望返回资产 ID=%s，实际 %s", id.Hex(), resp.Data.Id)
	}
	// 详情接口必须返回大字段（清单列表投影已排除，此处应按需补全）
	if resp.Data.HttpBody != "<html>full body</html>" {
		t.Errorf("详情应返回完整 body，实际 %q", resp.Data.HttpBody)
	}
	if resp.Data.HttpHeader != "HTTP/1.1 200 OK" {
		t.Errorf("详情应返回完整 header，实际 %q", resp.Data.HttpHeader)
	}
	if resp.Data.Banner != "nginx" {
		t.Errorf("详情应返回 banner，实际 %q", resp.Data.Banner)
	}
}

// TestAssetDetail_EmptyId 验证缺少资产 ID 时返回参数错误，不触发查询。
func TestAssetDetail_EmptyId(t *testing.T) {
	svcCtx, cleanup := newTestSvcCtxDB(t)
	defer cleanup()
	l := NewAssetDetailLogic(context.Background(), svcCtx)
	resp, err := l.AssetDetail(&types.AssetDetailReq{Id: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Code != 400 {
		t.Errorf("期望 Code=400（ID 为空），实际 %d", resp.Code)
	}
}
