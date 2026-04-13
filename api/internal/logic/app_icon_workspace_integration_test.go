package logic

import (
	"context"
	"testing"
	"time"

	"cscan/api/internal/middleware"
	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/model"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func TestAppListAggregatesAllWorkspaceData(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)
	defer client.Disconnect(ctx)

	db := client.Database("cscan_test")
	workspaceId := primitive.NewObjectID().Hex()
	cleanupCollections(t, db, workspaceId)
	defer cleanupCollections(t, db, workspaceId)

	workspace := &model.Workspace{
		Id:          mustObjectID(t, workspaceId),
		Name:        "ws-app-icon",
		Description: "test workspace",
		Status:      model.StatusEnable,
		CreateTime:  time.Now(),
		UpdateTime:  time.Now(),
	}
	err = model.NewWorkspaceModel(db).Insert(ctx, workspace)
	require.NoError(t, err)

	asset := &model.Asset{
		Id:         primitive.NewObjectID(),
		Authority:  "example.com:443",
		Host:       "example.com",
		Port:       443,
		App:        []string{"nginx[httpx]"},
		OrgId:      "",
		CreateTime: time.Now(),
		UpdateTime: time.Now(),
	}
	err = model.NewAssetModel(db, workspaceId).Insert(ctx, asset)
	require.NoError(t, err)

	logicCtx := context.WithValue(context.Background(), middleware.WorkspaceIdKey, "all")
	svcCtx := &svc.ServiceContext{
		MongoDB:           db,
		WorkspaceModel:    model.NewWorkspaceModel(db),
		OrganizationModel: model.NewOrganizationModel(db),
	}

	resp, err := NewAppListLogic(logicCtx, svcCtx).AppList(&types.AppListReq{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, int64(1), resp.Total)
	require.Len(t, resp.List, 1)
	require.Equal(t, "nginx[httpx]", resp.List[0].App)
}

func TestIconListAggregatesAllWorkspaceData(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)
	defer client.Disconnect(ctx)

	db := client.Database("cscan_test")
	workspaceId := primitive.NewObjectID().Hex()
	cleanupCollections(t, db, workspaceId)
	defer cleanupCollections(t, db, workspaceId)

	workspace := &model.Workspace{
		Id:          mustObjectID(t, workspaceId),
		Name:        "ws-icon-only",
		Description: "test workspace",
		Status:      model.StatusEnable,
		CreateTime:  time.Now(),
		UpdateTime:  time.Now(),
	}
	err = model.NewWorkspaceModel(db).Insert(ctx, workspace)
	require.NoError(t, err)

	asset := &model.Asset{
		Id:            primitive.NewObjectID(),
		Authority:     "icon.example.com:443",
		Host:          "icon.example.com",
		Port:          443,
		IconHash:      "123456789",
		IconHashFile:  "favicon.ico",
		IconHashBytes: []byte{1, 2, 3},
		CreateTime:    time.Now(),
		UpdateTime:    time.Now(),
	}
	err = model.NewAssetModel(db, workspaceId).Insert(ctx, asset)
	require.NoError(t, err)

	logicCtx := context.WithValue(context.Background(), middleware.WorkspaceIdKey, "all")
	svcCtx := &svc.ServiceContext{
		MongoDB:           db,
		WorkspaceModel:    model.NewWorkspaceModel(db),
		OrganizationModel: model.NewOrganizationModel(db),
	}

	resp, err := NewIconListLogic(logicCtx, svcCtx).IconList(&types.IconListReq{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, int64(1), resp.Total)
	require.Len(t, resp.List, 1)
	require.Equal(t, "123456789", resp.List[0].IconHash)
}

func TestIconListReturnsIconDataScreenshotAndUpdateTime(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)
	defer client.Disconnect(ctx)

	db := client.Database("cscan_test")
	workspaceId := primitive.NewObjectID().Hex()
	cleanupCollections(t, db, workspaceId)
	defer cleanupCollections(t, db, workspaceId)

	workspace := &model.Workspace{
		Id:          mustObjectID(t, workspaceId),
		Name:        "ws-icon-rich",
		Description: "test workspace",
		Status:      model.StatusEnable,
		CreateTime:  time.Now(),
		UpdateTime:  time.Now(),
	}
	err = model.NewWorkspaceModel(db).Insert(ctx, workspace)
	require.NoError(t, err)

	createTime := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	updateTime := time.Date(2026, 4, 2, 11, 0, 0, 0, time.UTC)
	assetId := primitive.NewObjectID()
	err = model.NewAssetModel(db, workspaceId).Insert(ctx, &model.Asset{
		Id:            assetId,
		Authority:     "icon.example.com:443",
		Host:          "icon.example.com",
		Port:          443,
		IconHash:      "123456789",
		IconHashFile:  "favicon.ico",
		IconHashBytes: []byte("png-bytes"),
		Screenshot:    "https://example.com/shot.png",
		CreateTime:    createTime,
		UpdateTime:    updateTime,
	})
	require.NoError(t, err)

	_, err = db.Collection(workspaceId+"_asset").UpdateOne(ctx, bson.M{"_id": assetId}, bson.M{"$set": bson.M{
		"create_time": createTime,
		"update_time": updateTime,
	}})
	require.NoError(t, err)

	logicCtx := context.WithValue(context.Background(), middleware.WorkspaceIdKey, "all")
	svcCtx := &svc.ServiceContext{
		MongoDB:           db,
		WorkspaceModel:    model.NewWorkspaceModel(db),
		OrganizationModel: model.NewOrganizationModel(db),
	}

	resp, err := NewIconListLogic(logicCtx, svcCtx).IconList(&types.IconListReq{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Len(t, resp.List, 1)
	require.Equal(t, "123456789", resp.List[0].IconHash)
	require.NotEmpty(t, resp.List[0].IconData)
	require.Equal(t, "https://example.com/shot.png", resp.List[0].Screenshot)
	require.Equal(t, "2026-04-01 10:00:00", resp.List[0].CreateTime)
	require.Equal(t, "2026-04-02 11:00:00", resp.List[0].UpdateTime)
}

func TestIconListFiltersRecordsWithoutIconBytes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)
	defer client.Disconnect(ctx)

	db := client.Database("cscan_test")
	workspaceId := primitive.NewObjectID().Hex()
	cleanupCollections(t, db, workspaceId)
	defer cleanupCollections(t, db, workspaceId)

	workspace := &model.Workspace{
		Id:          mustObjectID(t, workspaceId),
		Name:        "ws-icon-filter",
		Description: "test workspace",
		Status:      model.StatusEnable,
		CreateTime:  time.Now(),
		UpdateTime:  time.Now(),
	}
	err = model.NewWorkspaceModel(db).Insert(ctx, workspace)
	require.NoError(t, err)

	err = model.NewAssetModel(db, workspaceId).Insert(ctx, &model.Asset{
		Id:           primitive.NewObjectID(),
		Authority:    "empty.example.com:443",
		Host:         "empty.example.com",
		Port:         443,
		IconHash:     "no-bytes",
		IconHashFile: "favicon.ico",
		CreateTime:   time.Now(),
		UpdateTime:   time.Now(),
	})
	require.NoError(t, err)

	logicCtx := context.WithValue(context.Background(), middleware.WorkspaceIdKey, workspaceId)
	svcCtx := &svc.ServiceContext{MongoDB: db, WorkspaceModel: model.NewWorkspaceModel(db)}

	resp, err := NewIconListLogic(logicCtx, svcCtx).IconList(&types.IconListReq{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Zero(t, resp.Total)
	require.Empty(t, resp.List)
}

func TestAppDeleteRemovesAllWorkspaceData(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)
	defer client.Disconnect(ctx)

	db := client.Database("cscan_test")
	workspaceIds := []string{primitive.NewObjectID().Hex(), primitive.NewObjectID().Hex()}
	cleanupCollections(t, db, workspaceIds...)
	defer cleanupCollections(t, db, workspaceIds...)
	insertWorkspaces(t, db, workspaceIds)

	for i, workspaceId := range workspaceIds {
		err = model.NewAssetModel(db, workspaceId).Insert(ctx, &model.Asset{
			Id:         primitive.NewObjectID(),
			Authority:  "app.example.com:443",
			Host:       "app-" + string(rune('a'+i)) + ".example.com",
			Port:       443,
			App:        []string{"nginx[httpx]"},
			CreateTime: time.Now(),
			UpdateTime: time.Now(),
		})
		require.NoError(t, err)
	}

	logicCtx := context.WithValue(context.Background(), middleware.WorkspaceIdKey, "all")
	svcCtx := &svc.ServiceContext{MongoDB: db, WorkspaceModel: model.NewWorkspaceModel(db)}

	resp, err := NewAppListLogic(logicCtx, svcCtx).AppDelete(&types.AppDeleteReq{Id: "nginx[httpx]"})
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code)

	for _, workspaceId := range workspaceIds {
		count, err := model.NewAssetModel(db, workspaceId).Count(ctx, bson.M{"app": bson.M{"$in": []string{"nginx[httpx]"}}})
		require.NoError(t, err)
		require.Zero(t, count)
	}
}

func TestIconDeleteRemovesAllWorkspaceData(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)
	defer client.Disconnect(ctx)

	db := client.Database("cscan_test")
	workspaceIds := []string{primitive.NewObjectID().Hex(), primitive.NewObjectID().Hex()}
	cleanupCollections(t, db, workspaceIds...)
	defer cleanupCollections(t, db, workspaceIds...)
	insertWorkspaces(t, db, workspaceIds)

	for i, workspaceId := range workspaceIds {
		err = model.NewAssetModel(db, workspaceId).Insert(ctx, &model.Asset{
			Id:           primitive.NewObjectID(),
			Authority:    "icon.example.com:443",
			Host:         "icon-" + string(rune('a'+i)) + ".example.com",
			Port:         443,
			IconHash:     "123456789",
			CreateTime:   time.Now(),
			UpdateTime:   time.Now(),
			IconHashFile: "favicon.ico",
		})
		require.NoError(t, err)
	}

	logicCtx := context.WithValue(context.Background(), middleware.WorkspaceIdKey, "all")
	svcCtx := &svc.ServiceContext{MongoDB: db, WorkspaceModel: model.NewWorkspaceModel(db)}

	resp, err := NewIconListLogic(logicCtx, svcCtx).IconDelete(&types.IconDeleteReq{Id: "123456789"})
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code)

	for _, workspaceId := range workspaceIds {
		count, err := model.NewAssetModel(db, workspaceId).Count(ctx, bson.M{"icon_hash": "123456789"})
		require.NoError(t, err)
		require.Zero(t, count)
	}
}

func TestAppBatchDeleteRemovesAllWorkspaceData(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)
	defer client.Disconnect(ctx)

	db := client.Database("cscan_test")
	workspaceIds := []string{primitive.NewObjectID().Hex(), primitive.NewObjectID().Hex()}
	cleanupCollections(t, db, workspaceIds...)
	defer cleanupCollections(t, db, workspaceIds...)
	insertWorkspaces(t, db, workspaceIds)

	for _, workspaceId := range workspaceIds {
		err = model.NewAssetModel(db, workspaceId).Insert(ctx, &model.Asset{
			Id:         primitive.NewObjectID(),
			Authority:  "app.example.com:443",
			Host:       "batch." + workspaceId[:6] + ".example.com",
			Port:       443,
			App:        []string{"nginx[httpx]"},
			CreateTime: time.Now(),
			UpdateTime: time.Now(),
		})
		require.NoError(t, err)
	}

	logicCtx := context.WithValue(context.Background(), middleware.WorkspaceIdKey, "all")
	svcCtx := &svc.ServiceContext{MongoDB: db, WorkspaceModel: model.NewWorkspaceModel(db)}

	resp, err := NewAppListLogic(logicCtx, svcCtx).AppBatchDelete(&types.AppBatchDeleteReq{Ids: []string{"nginx[httpx]"}})
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code)

	for _, workspaceId := range workspaceIds {
		count, err := model.NewAssetModel(db, workspaceId).Count(ctx, bson.M{"app": bson.M{"$in": []string{"nginx[httpx]"}}})
		require.NoError(t, err)
		require.Zero(t, count)
	}
}

func TestIconClearRemovesAllWorkspaceData(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)
	defer client.Disconnect(ctx)

	db := client.Database("cscan_test")
	workspaceIds := []string{primitive.NewObjectID().Hex(), primitive.NewObjectID().Hex()}
	cleanupCollections(t, db, workspaceIds...)
	defer cleanupCollections(t, db, workspaceIds...)
	insertWorkspaces(t, db, workspaceIds)

	for _, workspaceId := range workspaceIds {
		err = model.NewAssetModel(db, workspaceId).Insert(ctx, &model.Asset{
			Id:           primitive.NewObjectID(),
			Authority:    "icon.example.com:443",
			Host:         "clear." + workspaceId[:6] + ".example.com",
			Port:         443,
			IconHash:     "123456789",
			CreateTime:   time.Now(),
			UpdateTime:   time.Now(),
			IconHashFile: "favicon.ico",
		})
		require.NoError(t, err)
	}

	logicCtx := context.WithValue(context.Background(), middleware.WorkspaceIdKey, "all")
	svcCtx := &svc.ServiceContext{MongoDB: db, WorkspaceModel: model.NewWorkspaceModel(db)}

	resp, err := NewIconListLogic(logicCtx, svcCtx).IconClear()
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code)

	for _, workspaceId := range workspaceIds {
		count, err := model.NewAssetModel(db, workspaceId).Count(ctx, bson.M{"icon_hash": bson.M{"$exists": true, "$ne": ""}})
		require.NoError(t, err)
		require.Zero(t, count)
	}
}

func TestAppClearRemovesAllWorkspaceData(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)
	defer client.Disconnect(ctx)

	db := client.Database("cscan_test")
	workspaceIds := []string{primitive.NewObjectID().Hex(), primitive.NewObjectID().Hex()}
	cleanupCollections(t, db, workspaceIds...)
	defer cleanupCollections(t, db, workspaceIds...)
	insertWorkspaces(t, db, workspaceIds)

	for _, workspaceId := range workspaceIds {
		err = model.NewAssetModel(db, workspaceId).Insert(ctx, &model.Asset{
			Id:         primitive.NewObjectID(),
			Authority:  "app.example.com:443",
			Host:       "clear." + workspaceId[:6] + ".example.com",
			Port:       443,
			App:        []string{"nginx[httpx]"},
			CreateTime: time.Now(),
			UpdateTime: time.Now(),
		})
		require.NoError(t, err)
	}

	logicCtx := context.WithValue(context.Background(), middleware.WorkspaceIdKey, "all")
	svcCtx := &svc.ServiceContext{MongoDB: db, WorkspaceModel: model.NewWorkspaceModel(db)}

	resp, err := NewAppListLogic(logicCtx, svcCtx).AppClear()
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code)

	for _, workspaceId := range workspaceIds {
		count, err := model.NewAssetModel(db, workspaceId).Count(ctx, bson.M{"app": bson.M{"$exists": true, "$ne": bson.A{}}})
		require.NoError(t, err)
		require.Zero(t, count)
	}
}

func TestIconBatchDeleteRemovesAllWorkspaceData(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)
	defer client.Disconnect(ctx)

	db := client.Database("cscan_test")
	workspaceIds := []string{primitive.NewObjectID().Hex(), primitive.NewObjectID().Hex()}
	cleanupCollections(t, db, workspaceIds...)
	defer cleanupCollections(t, db, workspaceIds...)
	insertWorkspaces(t, db, workspaceIds)

	for _, workspaceId := range workspaceIds {
		err = model.NewAssetModel(db, workspaceId).Insert(ctx, &model.Asset{
			Id:           primitive.NewObjectID(),
			Authority:    "icon.example.com:443",
			Host:         "batch." + workspaceId[:6] + ".example.com",
			Port:         443,
			IconHash:     "123456789",
			CreateTime:   time.Now(),
			UpdateTime:   time.Now(),
			IconHashFile: "favicon.ico",
		})
		require.NoError(t, err)
	}

	logicCtx := context.WithValue(context.Background(), middleware.WorkspaceIdKey, "all")
	svcCtx := &svc.ServiceContext{MongoDB: db, WorkspaceModel: model.NewWorkspaceModel(db)}

	resp, err := NewIconListLogic(logicCtx, svcCtx).IconBatchDelete(&types.IconBatchDeleteReq{Ids: []string{"123456789"}})
	require.NoError(t, err)
	require.Equal(t, 0, resp.Code)

	for _, workspaceId := range workspaceIds {
		count, err := model.NewAssetModel(db, workspaceId).Count(ctx, bson.M{"icon_hash": bson.M{"$in": []string{"123456789"}}})
		require.NoError(t, err)
		require.Zero(t, count)
	}
}

func cleanupCollections(t *testing.T, db *mongo.Database, workspaceIds ...string) {
	t.Helper()
	for _, workspaceId := range workspaceIds {
		_, err := db.Collection("workspace").DeleteMany(context.Background(), bson.M{"_id": mustObjectID(t, workspaceId)})
		require.NoError(t, err)
		_, err = db.Collection(workspaceId+"_asset").DeleteMany(context.Background(), bson.M{})
		require.NoError(t, err)
	}
	_, _ = db.Collection("all_asset").DeleteMany(context.Background(), bson.M{})
}

func insertWorkspaces(t *testing.T, db *mongo.Database, workspaceIds []string) {
	t.Helper()
	workspaceModel := model.NewWorkspaceModel(db)
	for _, workspaceId := range workspaceIds {
		err := workspaceModel.Insert(context.Background(), &model.Workspace{
			Id:          mustObjectID(t, workspaceId),
			Name:        "ws-" + workspaceId[:8],
			Description: "test workspace",
			Status:      model.StatusEnable,
			CreateTime:  time.Now(),
			UpdateTime:  time.Now(),
		})
		require.NoError(t, err)
	}
}

func mustObjectID(t *testing.T, hex string) primitive.ObjectID {
	t.Helper()
	oid, err := primitive.ObjectIDFromHex(hex)
	require.NoError(t, err)
	return oid
}
