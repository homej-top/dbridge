package service

import (
	"errors"

	"github.com/dbridge/dbridge/internal/middleware"
	"github.com/dbridge/dbridge/internal/repository"
	"github.com/dbridge/dbridge/internal/config"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

type AuthService struct {
	cfg *config.Config
}

func NewAuthService(cfg *config.Config) *AuthService {
	return &AuthService{cfg: cfg}
}

type LoginInput struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required,min=6"`
}

type LoginOutput struct {
	Token     string              `json:"token"`
	ExpiresIn int                 `json:"expires_in"`
	User      repository.User     `json:"user"`
}

func (s *AuthService) Login(input LoginInput) (*LoginOutput, error) {
	if input.Username == "" || input.Password == "" {
		return nil, errors.New("username and password are required")
	}

	var user repository.User
	if err := repository.DB.Where("username = ? AND status = 1", input.Username).First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("invalid username or password")
		}
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(input.Password)); err != nil {
		return nil, errors.New("invalid username or password")
	}

	token, err := middleware.GenerateJWT(&s.cfg.JWT, user.ID, user.Username, user.Role, user.TenantID)
	if err != nil {
		return nil, err
	}

	return &LoginOutput{
		Token:     token,
		ExpiresIn: s.cfg.JWT.ExpiresIn,
		User:      user,
	}, nil
}

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

type ChangePasswordInput struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
}

func (s *AuthService) ChangePassword(userID string, input ChangePasswordInput) error {
	var user repository.User
	if err := repository.DB.Where("id = ?", userID).First(&user).Error; err != nil {
		return errors.New("user not found")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(input.OldPassword)); err != nil {
		return errors.New("旧密码不正确")
	}

	hashed, err := HashPassword(input.NewPassword)
	if err != nil {
		return errors.New("密码加密失败")
	}

	if err := repository.DB.Model(&user).Update("password", hashed).Error; err != nil {
		return errors.New("更新密码失败")
	}
	return nil
}
