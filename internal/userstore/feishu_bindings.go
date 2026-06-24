// internal/userstore/feishu_bindings.go
package userstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrFeishuBindingNotFound is returned by Get* methods when no row exists.
var ErrFeishuBindingNotFound = errors.New("userstore: feishu binding not found")

// ErrFeishuAppIDConflict is returned by Upsert when another user already
// holds the same app_id (UNIQUE constraint on app_id_hash).
var ErrFeishuAppIDConflict = errors.New("userstore: feishu app_id already bound by another user")

// FeishuBindingCredentials carries the user-supplied secrets we encrypt
// before persisting. Returned by Get* with plaintext fields populated.
type FeishuBindingCredentials struct {
	AppID       string
	AppSecret   string
	EncryptKey  string
	VerifyToken string
}

// FeishuBinding is the full row.
type FeishuBinding struct {
	UserID    string
	AppIDHash string
	FeishuBindingCredentials
	OpenID     string
	BoundAt    int64 // unix seconds, 0 if not bound
	DisabledAt int64 // unix seconds, 0 if not disabled
	CreatedAt  int64
}

func hashAppID(appID string) string {
	sum := sha256.Sum256([]byte(appID))
	return hex.EncodeToString(sum[:])
}

// loadCipher snapshots the field-encryption cipher once per operation. A
// nil cipher (Feishu disabled) yields a clear error. Callers reuse the
// returned local so a concurrent SetSecretCipher(nil) can't turn the cipher
// nil between successive Encrypt/Decrypt calls within one method.
func (s *SQLiteStore) loadCipher() (*SecretCipher, error) {
	c := s.cipher.Load()
	if c == nil {
		return nil, fmt.Errorf("userstore: feishu not enabled (no secret cipher configured)")
	}
	return c, nil
}

// UpsertFeishuBinding inserts or replaces the row for userID. open_id /
// bound_at / disabled_at are preserved on upsert (only credentials are
// rewritten).
func (s *SQLiteStore) UpsertFeishuBinding(ctx context.Context, userID string, c FeishuBindingCredentials) error {
	cipher, err := s.loadCipher()
	if err != nil {
		return err
	}
	hash := hashAppID(c.AppID)
	encA, err := cipher.Encrypt([]byte(c.AppID))
	if err != nil {
		return fmt.Errorf("encrypt app_id: %w", err)
	}
	encS, err := cipher.Encrypt([]byte(c.AppSecret))
	if err != nil {
		return fmt.Errorf("encrypt app_secret: %w", err)
	}
	encK, err := cipher.Encrypt([]byte(c.EncryptKey))
	if err != nil {
		return fmt.Errorf("encrypt encrypt_key: %w", err)
	}
	encV, err := cipher.Encrypt([]byte(c.VerifyToken))
	if err != nil {
		return fmt.Errorf("encrypt verify_token: %w", err)
	}

	now := time.Now().Unix()
	_, err = s.db.ExecContext(ctx,
		s.dia.Rebind(`INSERT INTO feishu_bindings(user_id, app_id_hash, app_id_enc, app_secret_enc, encrypt_key_enc, verify_token_enc, created_at)
		 VALUES(?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET
		   app_id_hash      = excluded.app_id_hash,
		   app_id_enc       = excluded.app_id_enc,
		   app_secret_enc   = excluded.app_secret_enc,
		   encrypt_key_enc  = excluded.encrypt_key_enc,
		   verify_token_enc = excluded.verify_token_enc,
		   disabled_at      = NULL`),
		userID, hash, encA, encS, encK, encV, now,
	)
	if err != nil {
		if s.dia.IsUniqueViolation(err) {
			return ErrFeishuAppIDConflict
		}
		return fmt.Errorf("upsert feishu binding: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetFeishuBinding(ctx context.Context, userID string) (*FeishuBinding, error) {
	return s.getFeishuBinding(ctx,
		`SELECT user_id, app_id_hash, app_id_enc, app_secret_enc, encrypt_key_enc, verify_token_enc,
		        IFNULL(open_id, ''), IFNULL(bound_at, 0), IFNULL(disabled_at, 0), created_at
		 FROM feishu_bindings WHERE user_id = ?`,
		userID,
	)
}

func (s *SQLiteStore) GetFeishuBindingByAppIDHash(ctx context.Context, hash string) (*FeishuBinding, error) {
	return s.getFeishuBinding(ctx,
		`SELECT user_id, app_id_hash, app_id_enc, app_secret_enc, encrypt_key_enc, verify_token_enc,
		        IFNULL(open_id, ''), IFNULL(bound_at, 0), IFNULL(disabled_at, 0), created_at
		 FROM feishu_bindings WHERE app_id_hash = ?`,
		hash,
	)
}

func (s *SQLiteStore) getFeishuBinding(ctx context.Context, q string, arg string) (*FeishuBinding, error) {
	cipher, err := s.loadCipher()
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, s.dia.Rebind(q), arg)
	var b FeishuBinding
	var encA, encS, encK, encV []byte
	err = row.Scan(&b.UserID, &b.AppIDHash, &encA, &encS, &encK, &encV,
		&b.OpenID, &b.BoundAt, &b.DisabledAt, &b.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFeishuBindingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan feishu binding: %w", err)
	}
	if plain, err := cipher.Decrypt(encA); err != nil {
		return nil, fmt.Errorf("decrypt app_id: %w", err)
	} else {
		b.AppID = string(plain)
	}
	if plain, err := cipher.Decrypt(encS); err != nil {
		return nil, fmt.Errorf("decrypt app_secret: %w", err)
	} else {
		b.AppSecret = string(plain)
	}
	if plain, err := cipher.Decrypt(encK); err != nil {
		return nil, fmt.Errorf("decrypt encrypt_key: %w", err)
	} else {
		b.EncryptKey = string(plain)
	}
	if plain, err := cipher.Decrypt(encV); err != nil {
		return nil, fmt.Errorf("decrypt verify_token: %w", err)
	} else {
		b.VerifyToken = string(plain)
	}
	return &b, nil
}

func (s *SQLiteStore) MarkFeishuBindingBound(ctx context.Context, userID, openID string) error {
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`UPDATE feishu_bindings SET open_id = ?, bound_at = ? WHERE user_id = ?`),
		openID, now, userID,
	)
	if err != nil {
		return fmt.Errorf("mark bound: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrFeishuBindingNotFound
	}
	return nil
}

func (s *SQLiteStore) MarkFeishuBindingDisabled(ctx context.Context, userID string) error {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`UPDATE feishu_bindings SET disabled_at = ? WHERE user_id = ?`), now, userID)
	if err != nil {
		return fmt.Errorf("mark disabled: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ClearFeishuBindingDisabled(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`UPDATE feishu_bindings SET disabled_at = NULL WHERE user_id = ?`), userID)
	if err != nil {
		return fmt.Errorf("clear disabled: %w", err)
	}
	return nil
}

func (s *SQLiteStore) DeleteFeishuBinding(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`DELETE FROM feishu_bindings WHERE user_id = ?`), userID)
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	return nil
}
