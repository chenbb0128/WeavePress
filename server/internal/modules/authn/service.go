package authn

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	goredis "github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	"github.com/chenbb0128/weavepress/server/internal/config"
	"github.com/chenbb0128/weavepress/server/internal/modules/workspace"
	redisclient "github.com/chenbb0128/weavepress/server/internal/platform/redis"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUserDisabled       = errors.New("user disabled")
	ErrRateLimited        = errors.New("too many login attempts")
	ErrInvalidToken       = errors.New("invalid access token")
)

type Claims struct {
	Role workspace.Role `json:"role"`
	jwt.RegisteredClaims
}

type Session struct {
	AccessToken  string
	ExpiresIn    int64
	RefreshToken string
	RefreshUntil time.Time
	User         workspace.User
}

type Service struct {
	store workspace.Store
	redis *redisclient.Client
	cfg   config.AuthConfig
	now   func() time.Time
}

func New(store workspace.Store, redis *redisclient.Client, cfg config.AuthConfig) *Service {
	return &Service{store: store, redis: redis, cfg: cfg, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Login(ctx context.Context, username, password, ip, agent string) (Session, error) {
	username = strings.TrimSpace(username)
	if s.isRateLimited(ctx, username, ip) {
		return Session{}, ErrRateLimited
	}
	user, err := s.store.GetUserByUsername(ctx, username)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		s.recordFailure(ctx, username, ip)
		return Session{}, ErrInvalidCredentials
	}
	if user.Status != "active" {
		return Session{}, ErrUserDisabled
	}
	s.clearFailures(ctx, username, ip)
	return s.newSession(ctx, user.User, "", agent, ip)
}

func (s *Service) Refresh(ctx context.Context, raw, ip, agent string) (Session, error) {
	if raw == "" {
		return Session{}, workspace.ErrInvalidRefresh
	}
	oldHash := sha256.Sum256([]byte(raw))
	newRaw, newHash, err := newOpaqueToken()
	if err != nil {
		return Session{}, err
	}
	expires := s.now().Add(s.cfg.RefreshTTL)
	userID, err := s.store.RotateRefreshToken(ctx, oldHash, "", newHash, expires, agent, ip)
	if err != nil {
		return Session{}, err
	}
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return Session{}, err
	}
	if user.Status != "active" {
		_ = s.store.RevokeUserTokens(ctx, user.ID)
		return Session{}, ErrUserDisabled
	}
	access, err := s.issueAccess(user)
	if err != nil {
		return Session{}, err
	}
	return Session{AccessToken: access, ExpiresIn: int64(s.cfg.AccessTTL.Seconds()), RefreshToken: newRaw, RefreshUntil: expires, User: user}, nil
}

func (s *Service) Logout(ctx context.Context, raw string) error {
	if raw == "" {
		return nil
	}
	hash := sha256.Sum256([]byte(raw))
	return s.store.RevokeRefreshToken(ctx, hash)
}

func (s *Service) ParseAccessToken(raw string) (Claims, error) {
	var claims Claims
	token, err := jwt.ParseWithClaims(raw, &claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalidToken
		}
		return []byte(s.cfg.JWTSecret), nil
	}, jwt.WithIssuer("weavepress"), jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		return Claims{}, ErrInvalidToken
	}
	return claims, nil
}

func (s *Service) CreateUser(ctx context.Context, username, password, nickname string, role workspace.Role) (workspace.User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return workspace.User{}, err
	}
	return s.store.CreateUser(ctx, strings.TrimSpace(username), string(hash), strings.TrimSpace(nickname), role, "active")
}

func (s *Service) ResetPassword(ctx context.Context, id uint64, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}
	return s.store.UpdatePassword(ctx, id, string(hash))
}

func (s *Service) Permissions(role workspace.Role) []string {
	base := []string{"dashboard:view", "collection:create", "collection:view", "collection:retry", "article:view"}
	if role == workspace.RoleAdmin {
		return append(base, "user:view", "user:create", "user:update", "user:reset-password")
	}
	return base
}

func (s *Service) Config() config.AuthConfig { return s.cfg }

func (s *Service) newSession(ctx context.Context, user workspace.User, family, agent, ip string) (Session, error) {
	access, err := s.issueAccess(user)
	if err != nil {
		return Session{}, err
	}
	refresh, hash, err := newOpaqueToken()
	if err != nil {
		return Session{}, err
	}
	if family == "" {
		family, err = randomHex(16)
		if err != nil {
			return Session{}, err
		}
	}
	expires := s.now().Add(s.cfg.RefreshTTL)
	if err := s.store.CreateRefreshToken(ctx, user.ID, family, hash, expires, agent, ip); err != nil {
		return Session{}, err
	}
	return Session{AccessToken: access, ExpiresIn: int64(s.cfg.AccessTTL.Seconds()), RefreshToken: refresh, RefreshUntil: expires, User: user}, nil
}

func (s *Service) issueAccess(user workspace.User) (string, error) {
	now := s.now()
	jti, err := randomHex(16)
	if err != nil {
		return "", err
	}
	claims := Claims{Role: user.Role, RegisteredClaims: jwt.RegisteredClaims{
		Issuer: "weavepress", Subject: strconv.FormatUint(user.ID, 10), ID: jti,
		IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(s.cfg.AccessTTL)),
	}}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(s.cfg.JWTSecret))
}

func newOpaqueToken() (string, [32]byte, error) {
	raw, err := randomHex(32)
	if err != nil {
		return "", [32]byte{}, err
	}
	return raw, sha256.Sum256([]byte(raw)), nil
}

func randomHex(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(buffer), nil
}

func (s *Service) rateKeys(username, ip string) (string, string) {
	userHash := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(username))))
	ipHash := sha256.Sum256([]byte(strings.TrimSpace(ip)))
	if s.redis == nil {
		return "", ""
	}
	return s.redis.Keys().MustBuild("auth", "login", "user", hex.EncodeToString(userHash[:8])), s.redis.Keys().MustBuild("auth", "login", "ip", hex.EncodeToString(ipHash[:8]))
}

func (s *Service) isRateLimited(ctx context.Context, username, ip string) bool {
	if s.redis == nil {
		return false
	}
	userKey, ipKey := s.rateKeys(username, ip)
	values, err := s.redis.Redis.MGet(ctx, userKey, ipKey).Result()
	if err != nil && !errors.Is(err, goredis.Nil) {
		return false
	}
	for _, value := range values {
		if value == nil {
			continue
		}
		count, _ := strconv.Atoi(fmt.Sprint(value))
		if count >= 5 {
			return true
		}
	}
	return false
}

func (s *Service) recordFailure(ctx context.Context, username, ip string) {
	if s.redis == nil {
		return
	}
	userKey, ipKey := s.rateKeys(username, ip)
	pipe := s.redis.Redis.TxPipeline()
	for _, key := range []string{userKey, ipKey} {
		pipe.Incr(ctx, key)
		pipe.Expire(ctx, key, 15*time.Minute)
	}
	_, _ = pipe.Exec(ctx)
}

func (s *Service) clearFailures(ctx context.Context, username, ip string) {
	if s.redis == nil {
		return
	}
	userKey, ipKey := s.rateKeys(username, ip)
	_ = s.redis.Redis.Del(ctx, userKey, ipKey).Err()
}
