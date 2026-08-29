package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/fruto-platform/fruto/services/control-plane-api/internal/audit"
	"github.com/fruto-platform/fruto/services/control-plane-api/internal/identity"
	"github.com/jackc/pgx/v5"
)

type MFAStatus struct {
	TOTPEnabled  bool `json:"totpEnabled"`
	PasskeyCount int  `json:"passkeyCount"`
}

type AuthenticationChallenge struct {
	ID       int64
	User     identity.User
	Kind     string
	Payload  map[string]any
	Attempts int
}

type TOTPCredential struct {
	Reference    string
	Version      int64
	LastUsedStep int64
}

func (s *Store) UserMFAStatus(ctx context.Context, userID int64) (MFAStatus, error) {
	var status MFAStatus
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM totp_credentials WHERE user_id=$1),
		(SELECT count(*) FROM webauthn_credentials WHERE user_id=$1)`, userID).Scan(&status.TOTPEnabled, &status.PasskeyCount)
	return status, err
}

func (s *Store) CreateAuthenticationChallenge(ctx context.Context, challengeHash []byte, userID int64, kind string, payload map[string]any, expiresAt time.Time) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if kind == "TOTPEnrollment" {
		if _, err = tx.Exec(ctx, `UPDATE authentication_challenges SET consumed_at=now() WHERE user_id=$1 AND kind='TOTPEnrollment' AND consumed_at IS NULL`, userID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO authentication_challenges(challenge_hash,user_id,kind,payload_json,expires_at) VALUES($1,$2,$3,$4,$5)`, challengeHash, userID, kind, encoded, expiresAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) AuthenticationChallenge(ctx context.Context, challengeHash []byte, kind string) (AuthenticationChallenge, error) {
	var challenge AuthenticationChallenge
	var payload []byte
	row := s.Pool.QueryRow(ctx, `SELECT c.id,u.id,u.public_id,u.username,u.display_name,u.status,u.version,u.auth_version,u.created_at,u.updated_at,c.kind,c.payload_json,c.attempts
		FROM authentication_challenges c JOIN users u ON u.id=c.user_id
		WHERE c.challenge_hash=$1 AND c.kind=$2 AND c.consumed_at IS NULL AND c.expires_at>now() AND c.attempts<5 AND u.status='Active'`, challengeHash, kind)
	err := row.Scan(&challenge.ID, &challenge.User.ID, &challenge.User.PublicID, &challenge.User.Username, &challenge.User.DisplayName, &challenge.User.Status, &challenge.User.Version, &challenge.User.AuthVersion, &challenge.User.CreatedAt, &challenge.User.UpdatedAt, &challenge.Kind, &payload, &challenge.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthenticationChallenge{}, ErrNotFound
	}
	if err != nil {
		return AuthenticationChallenge{}, err
	}
	if err = json.Unmarshal(payload, &challenge.Payload); err != nil {
		return AuthenticationChallenge{}, err
	}
	return challenge, nil
}

func (s *Store) RecordAuthenticationChallengeFailure(ctx context.Context, challengeID int64) error {
	_, err := s.Pool.Exec(ctx, `UPDATE authentication_challenges SET attempts=LEAST(attempts+1,5),consumed_at=CASE WHEN attempts+1>=5 THEN now() ELSE consumed_at END WHERE id=$1`, challengeID)
	return err
}

func (s *Store) TOTPCredential(ctx context.Context, userID int64) (TOTPCredential, error) {
	var credential TOTPCredential
	err := s.Pool.QueryRow(ctx, `SELECT secret_reference,secret_version,last_used_step FROM totp_credentials WHERE user_id=$1`, userID).Scan(&credential.Reference, &credential.Version, &credential.LastUsedStep)
	if errors.Is(err, pgx.ErrNoRows) {
		return TOTPCredential{}, ErrNotFound
	}
	return credential, err
}

func (s *Store) EnableTOTP(ctx context.Context, challengeID, userID int64, reference string, version int64, recoveryCodeHashes [][]byte, event audit.Event) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `UPDATE authentication_challenges SET consumed_at=now() WHERE id=$1 AND user_id=$2 AND kind='TOTPEnrollment' AND consumed_at IS NULL AND expires_at>now()`, challengeID, userID)
	if err != nil || result.RowsAffected() != 1 {
		return ErrConflict
	}
	if _, err = tx.Exec(ctx, `INSERT INTO totp_credentials(user_id,secret_reference,secret_version) VALUES($1,$2,$3)
		ON CONFLICT(user_id) DO UPDATE SET secret_reference=EXCLUDED.secret_reference,secret_version=EXCLUDED.secret_version,enabled_at=now(),last_used_step=0`, userID, reference, version); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM recovery_codes WHERE user_id=$1`, userID); err != nil {
		return err
	}
	for _, codeHash := range recoveryCodeHashes {
		if _, err = tx.Exec(ctx, `INSERT INTO recovery_codes(user_id,code_hash) VALUES($1,$2)`, userID, codeHash); err != nil {
			return err
		}
	}
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) CompleteTOTPChallenge(ctx context.Context, challengeID, userID, step int64, event audit.Event) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `UPDATE totp_credentials SET last_used_step=$2 WHERE user_id=$1 AND last_used_step<$2`, userID, step)
	if err != nil || result.RowsAffected() != 1 {
		return ErrConflict
	}
	result, err = tx.Exec(ctx, `UPDATE authentication_challenges SET consumed_at=now() WHERE id=$1 AND user_id=$2 AND kind='Login' AND consumed_at IS NULL AND expires_at>now()`, challengeID, userID)
	if err != nil || result.RowsAffected() != 1 {
		return ErrConflict
	}
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) CompleteRecoveryChallenge(ctx context.Context, challengeID, userID int64, codeHash []byte, event audit.Event) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var recoveryID int64
	if err = tx.QueryRow(ctx, `SELECT id FROM recovery_codes WHERE user_id=$1 AND code_hash=$2 AND used_at IS NULL FOR UPDATE`, userID, codeHash).Scan(&recoveryID); errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE recovery_codes SET used_at=now() WHERE id=$1`, recoveryID); err != nil {
		return err
	}
	result, err := tx.Exec(ctx, `UPDATE authentication_challenges SET consumed_at=now() WHERE id=$1 AND user_id=$2 AND kind='Login' AND consumed_at IS NULL AND expires_at>now()`, challengeID, userID)
	if err != nil || result.RowsAffected() != 1 {
		return ErrConflict
	}
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) DisableTOTP(ctx context.Context, userID int64, event audit.Event) (string, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var reference string
	if err = tx.QueryRow(ctx, `DELETE FROM totp_credentials WHERE user_id=$1 RETURNING secret_reference`, userID).Scan(&reference); errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM recovery_codes WHERE user_id=$1`, userID); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET auth_version=auth_version+1,updated_at=now() WHERE id=$1`, userID); err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at=now() WHERE user_id=$1 AND revoked_at IS NULL`, userID); err != nil {
		return "", err
	}
	if err = insertAudit(ctx, tx, event); err != nil {
		return "", err
	}
	return reference, tx.Commit(ctx)
}
