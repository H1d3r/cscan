package model

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/bcrypt"
)

const (
	UserCollection = "user"

	StatusEnable  = "enable"
	StatusDisable = "disable"
)

type User struct {
	Id             primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Username       string             `bson:"username" json:"username"`
	Password       string             `bson:"password" json:"-"`
	Role           string             `bson:"role,omitempty" json:"role"`
	Status         string             `bson:"status" json:"status"`
	Avatar         string             `bson:"avatar,omitempty" json:"avatar"`
	Email          string             `bson:"email,omitempty" json:"email,omitempty"`
	Phone          string             `bson:"phone,omitempty" json:"phone,omitempty"`
	ScanConfig     string             `bson:"scan_config" json:"scanConfig"` // 用户默认扫描配置JSON
	LastLoginTime  *time.Time         `bson:"last_login_time" json:"lastLoginTime"`
	CreateTime     time.Time          `bson:"create_time" json:"createTime"`
	UpdateTime     time.Time          `bson:"update_time" json:"updateTime"`
	OnboardingDone bool               `bson:"onboarding_done,omitempty" json:"onboardingDone"` // 首次引导是否已完成（T4.2）
}

// IsSuperadmin 判断用户是否为管理员角色（superadmin 或 admin，状态/角色受保护，禁止降级）
func (u *User) IsSuperadmin() bool {
	return u != nil && (u.Role == "superadmin" || u.Role == "admin")
}

type UserModel struct {
	coll *mongo.Collection
}

func NewUserModel(db *mongo.Database) *UserModel {
	return &UserModel{
		coll: db.Collection(UserCollection),
	}
}

func (m *UserModel) Insert(ctx context.Context, doc *User) error {
	if doc.Id.IsZero() {
		doc.Id = primitive.NewObjectID()
	}
	now := time.Now()
	doc.CreateTime = now
	doc.UpdateTime = now
	hashed, err := HashPassword(doc.Password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	doc.Password = hashed
	_, err = m.coll.InsertOne(ctx, doc)
	return err
}

func (m *UserModel) FindByUsername(ctx context.Context, username string) (*User, error) {
	var doc User
	err := m.coll.FindOne(ctx, bson.M{"username": username}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil // 用户不存在，返回nil而不是错误
		}
		return nil, err // 其他错误
	}
	return &doc, nil
}

func (m *UserModel) FindById(ctx context.Context, id string) (*User, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	return m.FindByObjectId(ctx, oid)
}

// FindByObjectId 通过 ObjectID 查询用户（中间件 / PAT 路径使用）
func (m *UserModel) FindByObjectId(ctx context.Context, oid primitive.ObjectID) (*User, error) {
	var doc User
	err := m.coll.FindOne(ctx, bson.M{"_id": oid}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return &doc, nil
}

func (m *UserModel) Find(ctx context.Context, filter bson.M, page, pageSize int) ([]User, error) {
	page, pageSize = NormalizePage(page, pageSize)
	opts := options.Find()
	if page > 0 && pageSize > 0 {
		opts.SetSkip(int64((page - 1) * pageSize))
		opts.SetLimit(int64(pageSize))
	}
	opts.SetSort(bson.D{{Key: "create_time", Value: -1}})

	cursor, err := m.coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []User
	if err = cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *UserModel) Count(ctx context.Context, filter bson.M) (int64, error) {
	return m.coll.CountDocuments(ctx, filter)
}

func (m *UserModel) Update(ctx context.Context, id string, update bson.M) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	update["update_time"] = time.Now()
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	return err
}

func (m *UserModel) UpdatePassword(ctx context.Context, id string, newPassword string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	hashed, err := HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	update := bson.M{
		"password":    hashed,
		"update_time": time.Now(),
	}
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	return err
}

func (m *UserModel) UpdateScanConfig(ctx context.Context, id string, config string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	update := bson.M{
		"scan_config": config,
		"update_time": time.Now(),
	}
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	return err
}

// UpdateAvatar 更新用户头像（avatar 为相对路径或空串）
func (m *UserModel) UpdateAvatar(ctx context.Context, id string, avatar string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	update := bson.M{
		"avatar":      avatar,
		"update_time": time.Now(),
	}
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	return err
}

// SetOnboardingDone 标记用户首次引导已完成（T4.2），仅置位不覆盖其它字段
func (m *UserModel) SetOnboardingDone(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	update := bson.M{
		"onboarding_done": true,
		"update_time":     time.Now(),
	}
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": update})
	return err
}

// UpdateProfile 更新当前用户个人信息（仅允许 username/email/phone/avatar 字段）
func (m *UserModel) UpdateProfile(ctx context.Context, id string, username, email, phone, avatar string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	set := bson.M{"update_time": time.Now()}
	if username != "" {
		set["username"] = username
	}
	set["email"] = email
	set["phone"] = phone
	if avatar != "" {
		set["avatar"] = avatar
	}
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": set})
	return err
}

func (m *UserModel) GetScanConfig(ctx context.Context, id string) (string, error) {
	user, err := m.FindById(ctx, id)
	if err != nil {
		return "", err
	}
	if user == nil {
		return "", nil
	}
	return user.ScanConfig, nil
}

func (m *UserModel) DeleteById(ctx context.Context, id string) error {
	return m.Delete(ctx, id)
}

func (m *UserModel) UpdateLoginTime(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	now := time.Now()
	_, err = m.coll.UpdateOne(ctx, bson.M{"_id": oid}, bson.M{"$set": bson.M{"last_login_time": now}})
	return err
}

func (m *UserModel) Delete(ctx context.Context, id string) error {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	_, err = m.coll.DeleteOne(ctx, bson.M{"_id": oid})
	return err
}

// VerifyPassword 验证用户名和密码。
// 返回值：
//   - user: 用户信息（认证成功时非 nil）
//   - ok: 认证是否成功（用户存在 + 密码正确 + 用户启用）
//   - err: 非 nil 表示基础设施错误（如 MongoDB 故障、context 超时），调用方必须将其与认证失败区分开
//
// 重要：err != nil 时 ok 一定为 false，但 ok == false 不代表 err != nil。
// 调用方应优先检查 err，再检查 ok。
func (m *UserModel) VerifyPassword(ctx context.Context, username, password string) (*User, bool, error) {
	user, err := m.FindByUsername(ctx, username)
	if err != nil {
		return nil, false, err
	}
	if user == nil {
		return nil, false, nil
	}
	if !CheckPassword(password, user.Password) {
		return nil, false, nil
	}
	if user.Status != StatusEnable {
		return nil, false, nil
	}
	return user, true, nil
}

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// ValidatePasswordStrength 验证密码强度：至少8位，含大小写和数字
func ValidatePasswordStrength(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("密码长度不能少于8位")
	}
	hasUpper, hasLower, hasDigit := false, false, false
	for _, c := range password {
		switch {
		case c >= 'A' && c <= 'Z':
			hasUpper = true
		case c >= 'a' && c <= 'z':
			hasLower = true
		case c >= '0' && c <= '9':
			hasDigit = true
		}
	}
	if !hasUpper || !hasLower || !hasDigit {
		return fmt.Errorf("密码必须包含大写字母、小写字母和数字")
	}
	return nil
}

// CheckPassword 验证密码是否正确
func CheckPassword(password, hashedPassword string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}
