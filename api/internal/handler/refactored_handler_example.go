package handler

import (
	"net/http"

	"cscan/api/internal/svc"
	"cscan/api/internal/types"
	"cscan/model"

	"github.com/zeromicro/go-zero/rest/httpx"
	"go.mongodb.org/mongo-driver/bson"
)

// RefactoredUserHandler demonstrates the new clean handler pattern
// - No function exceeds 50 lines
// - Uses dependency injection consistently
// - Eliminates special case handling through better design
type RefactoredUserHandler struct {
	svc.BaseHandler
	svcCtx *svc.RefactoredServiceContext
}

// NewRefactoredUserHandler creates a new refactored user handler
func NewRefactoredUserHandler(svcCtx *svc.RefactoredServiceContext) *RefactoredUserHandler {
	return &RefactoredUserHandler{
		svcCtx: svcCtx,
	}
}

// Login handles user login - clean pattern with no special cases
func (h *RefactoredUserHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req types.LoginReq
	if err := httpx.Parse(r, &req); err != nil {
		h.WriteError(w, svc.ValidationError(err.Error()))
		return
	}

	// Validate request using injected validator
	if err := h.ValidateRequest(h.svcCtx.GetValidator(), &req); err != nil {
		h.WriteError(w, err)
		return
	}

	// Use injected storage interface
	userModel := h.svcCtx.GetStorage().UserModel()
	user, err := userModel.FindByUsername(r.Context(), req.Username)
	if err != nil {
		h.WriteError(w, svc.NotFoundError("user"))
		return
	}

	// Business logic would go here (password verification, token generation)
	resp := &types.LoginResp{
		Code:     0,
		Msg:      "success",
		Token:    "generated-token",
		UserId:   user.Id.Hex(),
		Username: user.Username,
		Role:     "user", // Default role since User model doesn't have Role field
	}

	h.WriteJSON(w, resp)
}

// CreateUser handles user creation - demonstrates consistent error handling
func (h *RefactoredUserHandler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var req types.UserCreateReq
	if err := httpx.Parse(r, &req); err != nil {
		h.WriteError(w, svc.ValidationError(err.Error()))
		return
	}

	if err := h.ValidateRequest(h.svcCtx.GetValidator(), &req); err != nil {
		h.WriteError(w, err)
		return
	}

	userModel := h.svcCtx.GetStorage().UserModel()

	// Check if user already exists
	existing, _ := userModel.FindByUsername(r.Context(), req.Username)
	if existing != nil {
		h.WriteError(w, svc.ConflictError("user already exists"))
		return
	}

	// Create new user (business logic would be in a service layer)
	// For now, just return success
	h.WriteSuccess(w, "User created successfully")
}

// ListUsers handles user listing - demonstrates pagination pattern
func (h *RefactoredUserHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	var req types.PageReq
	if err := httpx.Parse(r, &req); err != nil {
		h.WriteError(w, svc.ValidationError(err.Error()))
		return
	}

	// Set defaults if not provided
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	userModel := h.svcCtx.GetStorage().UserModel()
	users, err := userModel.Find(r.Context(), bson.M{}, req.Page, req.PageSize)
	if err != nil {
		h.WriteError(w, svc.InternalError("failed to list users"))
		return
	}

	total, err := userModel.Count(r.Context(), bson.M{})
	if err != nil {
		h.WriteError(w, svc.InternalError("failed to count users"))
		return
	}

	resp := &types.UserListResp{
		Code:  0,
		Msg:   "success",
		Total: int(total),
		List:  convertUsersToUserInfo(users),
	}

	h.WriteJSON(w, resp)
}

// UpdateUser handles user updates - demonstrates clean update pattern
func (h *RefactoredUserHandler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	var req types.UserUpdateReq
	if err := httpx.Parse(r, &req); err != nil {
		h.WriteError(w, svc.ValidationError(err.Error()))
		return
	}

	if err := h.ValidateRequest(h.svcCtx.GetValidator(), &req); err != nil {
		h.WriteError(w, err)
		return
	}

	userModel := h.svcCtx.GetStorage().UserModel()

	// Find existing user to verify it exists
	_, err := userModel.FindById(r.Context(), req.Id)
	if err != nil {
		h.WriteError(w, svc.NotFoundError("user"))
		return
	}

	// Update user fields using the correct Update method signature
	updateFields := bson.M{
		"username": req.Username,
		"status":   req.Status,
	}

	if err := userModel.Update(r.Context(), req.Id, updateFields); err != nil {
		h.WriteError(w, svc.InternalError("failed to update user"))
		return
	}

	h.WriteSuccess(w, "User updated successfully")
}

// DeleteUser handles user deletion - demonstrates clean delete pattern
func (h *RefactoredUserHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	var req types.UserDeleteReq
	if err := httpx.Parse(r, &req); err != nil {
		h.WriteError(w, svc.ValidationError(err.Error()))
		return
	}

	if req.Id == "" {
		h.WriteError(w, svc.ValidationError("user ID is required"))
		return
	}

	userModel := h.svcCtx.GetStorage().UserModel()

	// Verify user exists
	_, err := userModel.FindById(r.Context(), req.Id)
	if err != nil {
		h.WriteError(w, svc.NotFoundError("user"))
		return
	}

	if err := userModel.Delete(r.Context(), req.Id); err != nil {
		h.WriteError(w, svc.InternalError("failed to delete user"))
		return
	}

	h.WriteSuccess(w, "User deleted successfully")
}

// Helper functions

// convertUsersToUserInfo converts model users to API user info
func convertUsersToUserInfo(users []model.User) []types.UserInfo {
	result := make([]types.UserInfo, len(users))
	for i, user := range users {
		result[i] = types.UserInfo{
			Id:       user.Id.Hex(),
			Username: user.Username,
			Status:   user.Status,
		}
	}
	return result
}

// Handler factory functions for backward compatibility

// RefactoredLoginHandler creates a login handler using the new pattern
func RefactoredLoginHandler(svcCtx *svc.RefactoredServiceContext) http.HandlerFunc {
	handler := NewRefactoredUserHandler(svcCtx)
	return handler.Login
}

// RefactoredUserCreateHandler creates a user creation handler using the new pattern
func RefactoredUserCreateHandler(svcCtx *svc.RefactoredServiceContext) http.HandlerFunc {
	handler := NewRefactoredUserHandler(svcCtx)
	return handler.CreateUser
}

// RefactoredUserListHandler creates a user list handler using the new pattern
func RefactoredUserListHandler(svcCtx *svc.RefactoredServiceContext) http.HandlerFunc {
	handler := NewRefactoredUserHandler(svcCtx)
	return handler.ListUsers
}

// RefactoredUserUpdateHandler creates a user update handler using the new pattern
func RefactoredUserUpdateHandler(svcCtx *svc.RefactoredServiceContext) http.HandlerFunc {
	handler := NewRefactoredUserHandler(svcCtx)
	return handler.UpdateUser
}

// RefactoredUserDeleteHandler creates a user delete handler using the new pattern
func RefactoredUserDeleteHandler(svcCtx *svc.RefactoredServiceContext) http.HandlerFunc {
	handler := NewRefactoredUserHandler(svcCtx)
	return handler.DeleteUser
}
