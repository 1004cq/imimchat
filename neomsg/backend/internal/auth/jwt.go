package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID   int64  `json:"uid"`
	DeviceID string `json:"did,omitempty"`
	jwt.RegisteredClaims
}

func (s *Service) IssueAccessToken(userID int64, deviceID string, ttl time.Duration) (string, error) {
	if s.jwtSecret == "" {
		return "", fmt.Errorf("jwt secret not configured")
	}
	now := time.Now()
	claims := Claims{
		UserID:   userID,
		DeviceID: deviceID,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			Issuer:    "neomsg",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.jwtSecret))
}

func (s *Service) ValidateAccessToken(tokenStr string) (userID int64, deviceID string, err error) {
	if tokenStr == "" {
		return 0, "", fmt.Errorf("empty token")
	}
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(s.jwtSecret), nil
	})
	if err != nil {
		return 0, "", err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return 0, "", fmt.Errorf("invalid token")
	}
	return claims.UserID, claims.DeviceID, nil
}

// ValidateBindSession 校验 bind 请求中的 user/device 与 JWT 一致
func (s *Service) ValidateBindSession(token string, userID int64, deviceID string) error {
	uid, did, err := s.ValidateAccessToken(token)
	if err != nil {
		return err
	}
	if uid != userID {
		return fmt.Errorf("user_id mismatch")
	}
	if did != "" && deviceID != "" && did != deviceID {
		return fmt.Errorf("device_id mismatch")
	}
	return nil
}
