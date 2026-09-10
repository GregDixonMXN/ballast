// Package auth is the permission boundary. MVP: local bearer tokens +
// role checks, with an Authorizer interface shaped for a future OpenFGA
// backend (object/type/relation/subject tuples). OIDC verification plugs
// in at Authenticate without changing call sites.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Role gates dangerous actions (approve, merge, deploy, secret access).
type Role string

const (
	RoleViewer Role = "viewer"
	RoleDev    Role = "developer"
	RoleAdmin  Role = "admin"
)

// Identity is the authenticated caller (human, agent, or runner).
type Identity struct {
	ID    string
	Kind  string // human | agent | runner | system
	Roles []string
}

// Authorizer answers fine-grained checks; LocalAuthorizer is the MVP.
type Authorizer interface {
	Can(ctx context.Context, id Identity, object, relation string) bool
}

// LocalAuthorizer maps roles to capabilities. Replace with OpenFGA later.
type LocalAuthorizer struct{}

func (LocalAuthorizer) Can(_ context.Context, id Identity, _, relation string) bool {
	has := func(r string) bool {
		for _, x := range id.Roles {
			if x == r {
				return true
			}
		}
		return false
	}
	switch relation {
	case "approve", "merge", "deploy":
		return has(string(RoleAdmin)) || has(string(RoleDev))
	case "write":
		return has(string(RoleAdmin)) || has(string(RoleDev))
	default:
		return true
	}
}

// Tokens issues local dev tokens (Authorization: Bearer <tok>).
// Production replaces issuance with OIDC; parsing stays identical.
type Tokens struct {
	mu sync.Mutex
	t  map[string]Identity
}

func NewTokens() *Tokens { return &Tokens{t: map[string]Identity{}} }

// Mint creates a token for id (dev only; OIDC mints in prod).
func (t *Tokens) Mint(id Identity) string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	tok := hex.EncodeToString(b)
	t.mu.Lock()
	t.t[tok] = id
	t.mu.Unlock()
	return tok
}

// Parse validates "Bearer <tok>" (or raw tok) into an Identity.
func (t *Tokens) Parse(header string) (Identity, error) {
	tok := strings.TrimPrefix(strings.TrimSpace(header), "Bearer ")
	t.mu.Lock()
	defer t.mu.Unlock()
	id, ok := t.t[tok]
	if !ok {
		return Identity{}, errors.New("unauthorized")
	}
	return id, nil
}

// LoadOperator loads a private, persistent local operator credential. New files
// are created exclusively; credentials are never returned in logs.
func (t *Tokens) LoadOperator(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if info, e := os.Lstat(path); e == nil && (!info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0) {
		return errors.New("operator token must be a private regular file (0600)")
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		raw := make([]byte, 32)
		if _, err = rand.Read(raw); err != nil {
			return err
		}
		b = []byte(hex.EncodeToString(raw))
		f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		_, err = f.Write(b)
		if e = f.Close(); err == nil {
			err = e
		}
	}
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errors.New("operator token must be a private regular file (0600)")
	}
	tok := strings.TrimSpace(string(b))
	decoded, err := hex.DecodeString(tok)
	if err != nil || len(decoded) != 32 {
		return errors.New("invalid operator token file")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.t[tok] = Identity{ID: "operator", Kind: "human", Roles: []string{"admin"}}
	return nil
}

// ReadCredential checks file type and permissions before reading any bytes.
func ReadCredential(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("credential must be a private regular file (0600)")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(string(b))
	raw, err := hex.DecodeString(tok)
	if err != nil || len(raw) != 32 {
		return "", errors.New("invalid credential file")
	}
	return tok, nil
}
