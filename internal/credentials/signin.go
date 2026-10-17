package credentials

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"strings"
	"time"
)

// SaveRequest completes a helper-owned OAuth attempt. No tokens are returned to
// the caller; the validated record is saved atomically by the Go host.
type SaveRequest struct {
	File     string `json:"file"`
	ClientID string `json:"clientId"`
	HostID   string `json:"hostId"`
	Nonce    string `json:"nonce"`
	Tokens   Tokens `json:"tokens"`
}

type Account struct {
	File      string    `json:"file"`
	Email     string    `json:"email,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
}

func SaveChatGPT(ctx context.Context, request SaveRequest, opts Options) (Account, error) {
	if strings.TrimSpace(request.File) == "" || request.ClientID == "" || request.ClientID == "dynamic_agent_client" || request.HostID == "" || request.Nonce == "" || request.Tokens.Access == "" || request.Tokens.IDToken == "" || request.Tokens.ExpiresIn <= 0 {
		return Account{}, errors.New("incomplete ChatGPT sign-in result")
	}
	scopes := strings.Fields(request.Tokens.Scope)
	if !hasScope(scopes, PlanScope) {
		return Account{}, errors.New("ChatGPT plan usage was not authorized")
	}
	c := NewChatGPT(request.File, opts)
	d, err := c.discover(ctx)
	if err != nil {
		return Account{}, err
	}
	claims, err := c.verifyIDToken(ctx, d, request.Tokens.IDToken, request.ClientID, request.Nonce)
	if err != nil {
		return Account{}, err
	}
	now := time.Now().UTC()
	record := Record{Issuer: claims.Issuer, Subject: claims.Subject, Email: claims.Email, ClientID: request.ClientID, HostID: request.HostID,
		IDToken: request.Tokens.IDToken, Access: request.Tokens.Access, Refresh: request.Tokens.Refresh, TokenType: request.Tokens.TokenType,
		Scopes: scopes, SavedAt: now, ExpiresAt: now.Add(time.Duration(request.Tokens.ExpiresIn) * time.Second)}
	err = WithFileLock(ctx, request.File, func() error {
		if _, err := os.Stat(request.File); err == nil {
			old, err := Read(request.File)
			if err != nil {
				return err
			}
			if old.Issuer != record.Issuer || old.ClientID != record.ClientID || old.Subject != record.Subject {
				return errors.New("ChatGPT account changed; choose a separate credential file")
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return Write(request.File, record)
	})
	return Account{File: request.File, Email: claims.Email, ExpiresAt: record.ExpiresAt}, err
}

type claims struct {
	Issuer   string          `json:"iss"`
	Subject  string          `json:"sub"`
	Audience json.RawMessage `json:"aud"`
	Expires  int64           `json:"exp"`
	IssuedAt int64           `json:"iat"`
	Nonce    string          `json:"nonce"`
	Email    string          `json:"email"`
}

func (c *ChatGPT) verifyIDToken(ctx context.Context, d discovery, token, clientID, nonce string) (claims, error) {
	invalid := errors.New("ChatGPT ID token validation failed")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return claims{}, invalid
	}
	var header struct{ Algorithm, KeyID string }
	var rawHeader struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(data, &rawHeader) != nil || rawHeader.Algorithm != "RS256" || rawHeader.KeyID == "" {
		return claims{}, invalid
	}
	header.Algorithm, header.KeyID = rawHeader.Algorithm, rawHeader.KeyID
	var keys struct {
		Keys []struct {
			KeyID string `json:"kid"`
			Type  string `json:"kty"`
			N     string `json:"n"`
			E     string `json:"e"`
		} `json:"keys"`
	}
	if err := c.getJSON(ctx, d.JWKSURI, &keys); err != nil {
		return claims{}, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return claims{}, invalid
	}
	verified := false
	for _, key := range keys.Keys {
		if key.KeyID != header.KeyID || key.Type != "RSA" {
			continue
		}
		n, nErr := base64.RawURLEncoding.DecodeString(key.N)
		e, eErr := base64.RawURLEncoding.DecodeString(key.E)
		if nErr != nil || eErr != nil || len(n) == 0 || len(e) == 0 || len(e) > 4 {
			continue
		}
		exponent := 0
		for _, b := range e {
			exponent = exponent<<8 | int(b)
		}
		digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		pub := &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exponent}
		verified = rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], signature) == nil
		break
	}
	if !verified {
		return claims{}, invalid
	}
	data, err = base64.RawURLEncoding.DecodeString(parts[1])
	var result claims
	if err != nil || json.Unmarshal(data, &result) != nil {
		return claims{}, invalid
	}
	var audiences []string
	var audience string
	if json.Unmarshal(result.Audience, &audience) == nil {
		audiences = []string{audience}
	} else {
		_ = json.Unmarshal(result.Audience, &audiences)
	}
	found := false
	for _, value := range audiences {
		found = found || value == clientID
	}
	now := time.Now().Unix()
	if !found || result.Issuer != d.Issuer || result.Subject == "" || result.Nonce != nonce || result.Expires < now-60 || result.IssuedAt > now+60 || result.IssuedAt == 0 {
		return claims{}, invalid
	}
	return result, nil
}
