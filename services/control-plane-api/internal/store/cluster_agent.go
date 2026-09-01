package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/audit"
)

var (
	ErrAgentEnrollmentInvalid  = errors.New("agent enrollment is invalid")
	ErrAgentEnrollmentConsumed = errors.New("agent enrollment was already consumed")
	ErrAgentIdentityMismatch   = errors.New("agent identity does not match")
)

type AgentInstallation struct {
	ID                     int64
	PublicID               string
	Name                   string
	Status                 string
	CertificateFingerprint []byte
	CertificateNotAfter    *time.Time
	LastSeenAt             *time.Time
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type AgentCertificate struct {
	InstallationID   string
	CertificatePEM   []byte
	CACertificatePEM []byte
	Serial           string
	Fingerprint      []byte
	NotAfter         time.Time
}

func (s *Store) CreateAgentInstallation(ctx context.Context, publicID, name string, tokenHash []byte, expiresAt time.Time, event audit.Event) (AgentInstallation, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 || len(tokenHash) == 0 || !expiresAt.After(time.Now()) {
		return AgentInstallation{}, ErrAgentEnrollmentInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return AgentInstallation{}, err
	}
	defer tx.Rollback(ctx)
	var value AgentInstallation
	if err = tx.QueryRow(ctx, `INSERT INTO agent_installations(public_id,name,created_by) VALUES($1,$2,$3)
		RETURNING id,public_id,name,status,created_at,updated_at`, publicID, name, event.ActorUserID).Scan(&value.ID, &value.PublicID, &value.Name, &value.Status, &value.CreatedAt, &value.UpdatedAt); err != nil {
		return AgentInstallation{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO agent_enrollment_tokens(installation_id,token_hash,expires_at) VALUES($1,$2,$3)`, value.ID, tokenHash, expiresAt); err != nil {
		return AgentInstallation{}, err
	}
	event.TargetPublicID = value.PublicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return AgentInstallation{}, err
	}
	return value, tx.Commit(ctx)
}

func (s *Store) EnrollAgent(ctx context.Context, tokenHash []byte, attemptID string, csrFingerprint []byte, now time.Time, issue func(string) (AgentCertificate, error), event audit.Event) (AgentCertificate, error) {
	if len(tokenHash) == 0 || strings.TrimSpace(attemptID) == "" || len(csrFingerprint) == 0 || issue == nil {
		return AgentCertificate{}, ErrAgentEnrollmentInvalid
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return AgentCertificate{}, err
	}
	defer tx.Rollback(ctx)
	var installation AgentInstallation
	var expiresAt time.Time
	var consumedAt *time.Time
	var storedAttempt *string
	var storedCSR, certificatePEM, caPEM, certificateFingerprint []byte
	var serial *string
	var notAfter *time.Time
	err = tx.QueryRow(ctx, `SELECT i.id,i.public_id,i.name,i.status,t.expires_at,t.consumed_at,i.enrollment_attempt_id,i.csr_fingerprint,
		i.certificate_pem,i.ca_certificate_pem,i.certificate_serial,i.certificate_fingerprint,i.certificate_not_after
		FROM agent_enrollment_tokens t JOIN agent_installations i ON i.id=t.installation_id
		WHERE t.token_hash=$1 FOR UPDATE OF t,i`, tokenHash).Scan(&installation.ID, &installation.PublicID, &installation.Name, &installation.Status, &expiresAt, &consumedAt, &storedAttempt, &storedCSR, &certificatePEM, &caPEM, &serial, &certificateFingerprint, &notAfter)
	if errors.Is(err, pgx.ErrNoRows) || (!expiresAt.After(now) && consumedAt == nil) || installation.Status == "Revoked" {
		return AgentCertificate{}, ErrAgentEnrollmentInvalid
	}
	if err != nil {
		return AgentCertificate{}, err
	}
	if consumedAt != nil {
		if storedAttempt != nil && *storedAttempt == attemptID && bytes.Equal(storedCSR, csrFingerprint) && serial != nil && notAfter != nil {
			return AgentCertificate{InstallationID: installation.PublicID, CertificatePEM: certificatePEM, CACertificatePEM: caPEM, Serial: *serial, Fingerprint: certificateFingerprint, NotAfter: *notAfter}, tx.Commit(ctx)
		}
		return AgentCertificate{}, ErrAgentEnrollmentConsumed
	}
	issued, err := issue(installation.PublicID)
	if err != nil {
		return AgentCertificate{}, err
	}
	if len(issued.CertificatePEM) == 0 || len(issued.CACertificatePEM) == 0 || issued.Serial == "" || len(issued.Fingerprint) == 0 || !issued.NotAfter.After(now) {
		return AgentCertificate{}, fmt.Errorf("issued agent certificate is incomplete")
	}
	issued.InstallationID = installation.PublicID
	if _, err = tx.Exec(ctx, `UPDATE agent_installations SET enrollment_attempt_id=$2,csr_fingerprint=$3,certificate_pem=$4,ca_certificate_pem=$5,
		certificate_serial=$6,certificate_fingerprint=$7,certificate_not_after=$8,updated_at=$9 WHERE id=$1`, installation.ID, attemptID, csrFingerprint,
		issued.CertificatePEM, issued.CACertificatePEM, issued.Serial, issued.Fingerprint, issued.NotAfter, now); err != nil {
		return AgentCertificate{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE agent_enrollment_tokens SET consumed_at=$2 WHERE installation_id=$1`, installation.ID, now); err != nil {
		return AgentCertificate{}, err
	}
	event.TargetPublicID = installation.PublicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return AgentCertificate{}, err
	}
	return issued, tx.Commit(ctx)
}

func (s *Store) ActivateAgent(ctx context.Context, publicID string, fingerprint []byte, now time.Time, event audit.Event) (bool, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var status string
	var storedFingerprint []byte
	var notAfter time.Time
	if err = tx.QueryRow(ctx, `SELECT status,certificate_fingerprint,certificate_not_after FROM agent_installations WHERE public_id=$1 FOR UPDATE`, publicID).Scan(&status, &storedFingerprint, &notAfter); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrAgentIdentityMismatch
		}
		return false, err
	}
	if status == "Revoked" || !bytes.Equal(storedFingerprint, fingerprint) || !notAfter.After(now) {
		return false, ErrAgentIdentityMismatch
	}
	first := status == "Pending"
	if _, err = tx.Exec(ctx, `UPDATE agent_installations SET status='Active',last_seen_at=$2,updated_at=$2 WHERE public_id=$1`, publicID, now); err != nil {
		return false, err
	}
	if first {
		event.TargetPublicID = publicID
		if err = insertAudit(ctx, tx, event); err != nil {
			return false, err
		}
	}
	return first, tx.Commit(ctx)
}

func (s *Store) TouchAgent(ctx context.Context, publicID string, fingerprint []byte, now time.Time) error {
	result, err := s.Pool.Exec(ctx, `UPDATE agent_installations SET last_seen_at=$3,updated_at=$3
		WHERE public_id=$1 AND status='Active' AND certificate_fingerprint=$2 AND certificate_not_after>$3`, publicID, fingerprint, now)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrAgentIdentityMismatch
	}
	return nil
}
