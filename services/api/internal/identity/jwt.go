package identity

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// JWTManager issues and verifies access tokens.
type JWTManager struct {
	secret []byte
	expiry time.Duration
	now    func() time.Time
}

func NewJWTManager(secret string, expiry time.Duration) *JWTManager {
	return &JWTManager{secret: []byte(secret), expiry: expiry, now: time.Now}
}

// Expiry returns the access-token TTL.
func (m *JWTManager) Expiry() time.Duration { return m.expiry }

// Issue creates a signed access token for the claims.
func (m *JWTManager) Issue(c SessionClaims) (string, error) {
	now := m.now()
	c.IssuedAt = now.Unix()
	c.ExpiresAt = now.Add(m.expiry).Unix()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":    c.UserID,
		"email":  c.Email,
		"school": c.SchoolID,
		"roles":  c.Roles,
		"pv":     c.PermVer,
		"iat":    c.IssuedAt,
		"exp":    c.ExpiresAt,
	})
	return token.SignedString(m.secret)
}

var (
	ErrInvalidToken = errors.New("identity: invalid token")
	ErrExpiredToken = errors.New("identity: token expired")
)

// Verify parses and validates a token, returning its claims.
func (m *JWTManager) Verify(tokenStr string) (*SessionClaims, error) {
	tok, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("%w: unexpected signing method %v", ErrInvalidToken, t.Header["alg"])
		}
		return m.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrExpiredToken
		}
		return nil, ErrInvalidToken
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok || !tok.Valid {
		return nil, ErrInvalidToken
	}
	sub, _ := claims["sub"].(string)
	if sub == "" {
		return nil, ErrInvalidToken
	}
	out := &SessionClaims{UserID: sub}
	if v, ok := claims["email"].(string); ok {
		out.Email = v
	}
	if v, ok := claims["school"].(string); ok {
		out.SchoolID = v
	}
	if v, ok := claims["pv"].(float64); ok {
		out.PermVer = int(v)
	}
	if rawRoles, ok := claims["roles"].([]any); ok {
		roles := make([]string, 0, len(rawRoles))
		for _, r := range rawRoles {
			if s, ok := r.(string); ok {
				roles = append(roles, s)
			}
		}
		out.Roles = roles
	}
	return out, nil
}
