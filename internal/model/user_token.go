package model

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	PATPrefix        = "cscan_pat_"
	MaxTokensPerUser = 10
)

type UserToken struct {
	Id         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserId     primitive.ObjectID `bson:"user_id" json:"userId"`
	Name       string             `bson:"name" json:"name"`
	TokenHash  string             `bson:"token_hash" json:"-"`
	Prefix     string             `bson:"prefix" json:"prefix"`
	Scopes     []string           `bson:"scopes,omitempty" json:"scopes,omitempty"`
	ExpiresAt  *time.Time         `bson:"expires_at,omitempty" json:"expiresAt,omitempty"`
	LastUsedAt *time.Time         `bson:"last_used_at,omitempty" json:"lastUsedAt,omitempty"`
	LastUsedIP string             `bson:"last_used_ip,omitempty" json:"lastUsedIp,omitempty"`
	Status     string             `bson:"status" json:"status"`
	CreateTime time.Time          `bson:"create_time" json:"createTime"`
	UpdateTime time.Time          `bson:"update_time" json:"updateTime"`
}

type UserTokenModel struct {
	coll *mongo.Collection
}

func NewUserTokenModel(db *mongo.Database) *UserTokenModel {
	coll := db.Collection("user_tokens")
	if err := ensureIndexes(coll, []mongo.IndexModel{
		{Keys: bson.D{{Key: "token_hash", Value: 1}}, Options: options.Index().SetUnique(true)},
	}); err != nil {
		logx.Errorf("[UserTokenModel] ensure token hash index failed: %v", err)
	}
	return &UserTokenModel{coll: coll}
}

func (m *UserTokenModel) Collection() *mongo.Collection {
	return m.coll
}

// RemoveLegacyPlainTokens permanently removes PAT plaintext left by older releases.
// The existence filter makes this migration safe to run on every startup.
func (m *UserTokenModel) RemoveLegacyPlainTokens(ctx context.Context) (int64, error) {
	result, err := m.coll.UpdateMany(
		ctx,
		bson.M{"plain_token": bson.M{"$exists": true}},
		bson.M{"$unset": bson.M{"plain_token": ""}},
	)
	if err != nil {
		return 0, err
	}
	return result.ModifiedCount, nil
}

func (m *UserTokenModel) Insert(ctx context.Context, doc *UserToken) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	now := time.Now()
	doc.CreateTime = now
	doc.UpdateTime = now
	if doc.Status == "" {
		doc.Status = StatusEnable
	}
	if doc.Scopes == nil {
		doc.Scopes = []string{"*"}
	}
	_, err := m.coll.InsertOne(ctx, doc)
	return err
}

func (m *UserTokenModel) FindByHash(ctx context.Context, hash string) (*UserToken, error) {
	var doc UserToken
	err := m.coll.FindOne(ctx, bson.M{"token_hash": hash}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

func (m *UserTokenModel) FindByUserId(ctx context.Context, userId primitive.ObjectID) ([]*UserToken, error) {
	opts := options.Find().SetSort(bson.D{{Key: "create_time", Value: -1}})
	cursor, err := m.coll.Find(ctx, bson.M{"user_id": userId}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []*UserToken
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *UserTokenModel) FindByIdAndUserId(ctx context.Context, id, userId primitive.ObjectID) (*UserToken, error) {
	var doc UserToken
	err := m.coll.FindOne(ctx, bson.M{"_id": id, "user_id": userId}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

func (m *UserTokenModel) UpdateLastUsed(ctx context.Context, id primitive.ObjectID, ip string, at time.Time) error {
	update := bson.M{
		"last_used_at": at,
		"update_time":  at,
	}
	if ip != "" {
		update["last_used_ip"] = ip
	}
	_, err := m.coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": update})
	return err
}

// SetStatusById 按 id + userId 切换 PAT 启用状态（enable / disable）。
func (m *UserTokenModel) SetStatusById(ctx context.Context, id, userId primitive.ObjectID, status string) error {
	now := time.Now()
	_, err := m.coll.UpdateOne(
		ctx,
		bson.M{"_id": id, "user_id": userId},
		bson.M{"$set": bson.M{"status": status, "update_time": now}},
	)
	return err
}

func (m *UserTokenModel) RevokeByUserId(ctx context.Context, userId primitive.ObjectID) (int64, error) {
	now := time.Now()
	res, err := m.coll.UpdateMany(
		ctx,
		bson.M{"user_id": userId, "status": StatusEnable},
		bson.M{"$set": bson.M{"status": StatusDisable, "update_time": now}},
	)
	if err != nil {
		return 0, err
	}
	return res.ModifiedCount, nil
}

func (m *UserTokenModel) CountByUserId(ctx context.Context, userId primitive.ObjectID) (int64, error) {
	return m.coll.CountDocuments(ctx, bson.M{"user_id": userId, "status": StatusEnable})
}

func GeneratePAT() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate pat: %w", err)
	}
	return PATPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

func HashPAT(token string) string {
	sum := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", sum[:])
}

func PATPrefixOf(token string) string {
	if len(token) <= 12 {
		return token
	}
	return token[:12]
}
