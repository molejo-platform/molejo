package store

import (
	"bytes"
	"context"
	"encoding/json"
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
	ErrClusterNotFound         = errors.New("cluster was not found")
	ErrClusterNotPending       = errors.New("cluster is not pending enrollment")
)

const agentCredentialOverlap = time.Hour

// AgentInstallation is the durable cluster record. Its historical name is kept
// at the storage boundary so existing databases and public IDs migrate safely.
type AgentInstallation struct {
	ID                int64      `json:"-"`
	PublicID          string     `json:"id"`
	Name              string     `json:"name"`
	Status            string     `json:"status"`
	ClusterUID        string     `json:"clusterUid,omitempty"`
	AgentVersion      string     `json:"agentVersion,omitempty"`
	KubernetesVersion string     `json:"kubernetesVersion,omitempty"`
	Capabilities      []string   `json:"capabilities"`
	CertificateExpiry *time.Time `json:"certificateExpiresAt,omitempty"`
	LastSeenAt        *time.Time `json:"lastSeenAt,omitempty"`
	RevokedAt         *time.Time `json:"revokedAt,omitempty"`
	RevocationReason  string     `json:"revocationReason,omitempty"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

type AgentCertificate struct {
	InstallationID   string
	CertificatePEM   []byte
	CACertificatePEM []byte
	ServerCAPEM      []byte
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

// EnsureBootstrapAgentInstallation creates the first cluster invitation or
// refreshes its unconsumed token while the cluster is pending.
func (s *Store) EnsureBootstrapAgentInstallation(ctx context.Context, publicID, name string, tokenHash []byte, expiresAt time.Time) error {
	name = strings.TrimSpace(name)
	if publicID == "" || name == "" || len(name) > 80 || len(tokenHash) == 0 || !expiresAt.After(time.Now()) {
		return ErrAgentEnrollmentInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var installationID int64
	var status string
	err = tx.QueryRow(ctx, `SELECT id,status FROM agent_installations WHERE public_id=$1 FOR UPDATE`, publicID).Scan(&installationID, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		var ownerID int64
		if err = tx.QueryRow(ctx, `SELECT u.id FROM users u
			JOIN installation_role_assignments r ON r.user_id=u.id AND r.role='Administrator'
			WHERE u.status='Active' ORDER BY u.id LIMIT 1`).Scan(&ownerID); err != nil {
			return fmt.Errorf("find bootstrap owner: %w", err)
		}
		if err = tx.QueryRow(ctx, `INSERT INTO agent_installations(public_id,name,created_by) VALUES($1,$2,$3) RETURNING id`, publicID, name, ownerID).Scan(&installationID); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO agent_enrollment_tokens(installation_id,token_hash,expires_at) VALUES($1,$2,$3)`, installationID, tokenHash, expiresAt); err != nil {
			return err
		}
		if err = assignLegacyPendingOperations(ctx, tx, installationID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	if status == "Active" {
		if err = assignLegacyPendingOperations(ctx, tx, installationID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	if status != "Pending" {
		return ErrAgentEnrollmentInvalid
	}
	result, err := tx.Exec(ctx, `UPDATE agent_enrollment_tokens SET token_hash=$2,expires_at=$3
		WHERE installation_id=$1 AND consumed_at IS NULL`, installationID, tokenHash, expiresAt)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrAgentEnrollmentConsumed
	}
	if err = assignLegacyPendingOperations(ctx, tx, installationID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func assignLegacyPendingOperations(ctx context.Context, tx pgx.Tx, installationID int64) error {
	if _, err := tx.Exec(ctx, `UPDATE operations SET agent_installation_id=$1::bigint,updated_at=now() WHERE agent_installation_id IS NULL AND status='Pending'`, installationID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO workspace_clusters(workspace_id,installation_id,namespace_name,state)
		SELECT DISTINCT o.workspace_id,$1::bigint,w.namespace_name,
			CASE WHEN w.bootstrap_state='Ready' THEN 'Ready' WHEN w.bootstrap_state='Running' THEN 'Running' WHEN w.bootstrap_state='Failed' THEN 'Failed' ELSE 'Pending' END
		FROM operations o JOIN workspaces w ON w.id=o.workspace_id
		WHERE o.agent_installation_id=$1::bigint AND o.kind='EnsureWorkspace'
		ON CONFLICT(workspace_id,installation_id) DO NOTHING`, installationID)
	return err
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
	err = tx.QueryRow(ctx, `SELECT i.id,i.public_id,i.name,i.status,t.expires_at,t.consumed_at
		FROM agent_enrollment_tokens t JOIN agent_installations i ON i.id=t.installation_id
		WHERE t.token_hash=$1 FOR UPDATE OF t,i`, tokenHash).Scan(&installation.ID, &installation.PublicID, &installation.Name, &installation.Status, &expiresAt, &consumedAt)
	if errors.Is(err, pgx.ErrNoRows) || (!expiresAt.After(now) && consumedAt == nil) || installation.Status == "Revoked" {
		return AgentCertificate{}, ErrAgentEnrollmentInvalid
	}
	if err != nil {
		return AgentCertificate{}, err
	}
	if consumedAt != nil {
		certificate, found, findErr := credentialForAttempt(ctx, tx, installation.ID, attemptID, csrFingerprint)
		if findErr != nil {
			return AgentCertificate{}, findErr
		}
		if found {
			certificate.InstallationID = installation.PublicID
			return certificate, tx.Commit(ctx)
		}
		return AgentCertificate{}, ErrAgentEnrollmentConsumed
	}
	issued, err := issue(installation.PublicID)
	if err != nil {
		return AgentCertificate{}, err
	}
	if err = validateIssuedCredential(issued, now); err != nil {
		return AgentCertificate{}, err
	}
	issued.InstallationID = installation.PublicID
	if err = insertAgentCredential(ctx, tx, installation.ID, "Active", attemptID, csrFingerprint, issued, now); err != nil {
		return AgentCertificate{}, err
	}
	// Keep the old columns populated during the alpha migration. They are no
	// longer authoritative and can be removed after all supported versions use
	// agent_credentials.
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

// RenewAgent rotates the client keypair while the current credential remains
// valid for a bounded overlap. Replaying the same attempt and CSR is idempotent.
func (s *Store) RenewAgent(ctx context.Context, publicID string, currentFingerprint []byte, attemptID string, csrFingerprint []byte, now time.Time, issue func(string) (AgentCertificate, error), event audit.Event) (AgentCertificate, error) {
	if publicID == "" || len(currentFingerprint) == 0 || strings.TrimSpace(attemptID) == "" || len(csrFingerprint) == 0 || issue == nil {
		return AgentCertificate{}, ErrAgentIdentityMismatch
	}
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return AgentCertificate{}, err
	}
	defer tx.Rollback(ctx)
	var installationID int64
	var installationStatus string
	err = tx.QueryRow(ctx, `SELECT id,status FROM agent_installations WHERE public_id=$1 FOR UPDATE`, publicID).Scan(&installationID, &installationStatus)
	if errors.Is(err, pgx.ErrNoRows) || installationStatus != "Active" {
		return AgentCertificate{}, ErrAgentIdentityMismatch
	}
	if err != nil {
		return AgentCertificate{}, err
	}
	if _, err = validCredential(ctx, tx, installationID, currentFingerprint, now); err != nil {
		return AgentCertificate{}, err
	}
	if existing, found, findErr := credentialForAttempt(ctx, tx, installationID, attemptID, csrFingerprint); findErr != nil {
		return AgentCertificate{}, findErr
	} else if found {
		existing.InstallationID = publicID
		return existing, tx.Commit(ctx)
	}
	issued, err := issue(publicID)
	if err != nil {
		return AgentCertificate{}, err
	}
	if err = validateIssuedCredential(issued, now); err != nil {
		return AgentCertificate{}, err
	}
	issued.InstallationID = publicID
	overlapUntil := now.Add(agentCredentialOverlap)
	if _, err = tx.Exec(ctx, `UPDATE agent_credentials SET status='Superseded',overlap_not_after=LEAST(certificate_not_after,$2),updated_at=$3
		WHERE installation_id=$1 AND status='Active'`, installationID, overlapUntil, now); err != nil {
		return AgentCertificate{}, err
	}
	if err = insertAgentCredential(ctx, tx, installationID, "Active", attemptID, csrFingerprint, issued, now); err != nil {
		return AgentCertificate{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE agent_installations SET certificate_pem=$2,ca_certificate_pem=$3,certificate_serial=$4,
		certificate_fingerprint=$5,certificate_not_after=$6,updated_at=$7 WHERE id=$1`, installationID, issued.CertificatePEM,
		issued.CACertificatePEM, issued.Serial, issued.Fingerprint, issued.NotAfter, now); err != nil {
		return AgentCertificate{}, err
	}
	event.TargetPublicID = publicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return AgentCertificate{}, err
	}
	return issued, tx.Commit(ctx)
}

func validateIssuedCredential(issued AgentCertificate, now time.Time) error {
	if len(issued.ServerCAPEM) == 0 {
		issued.ServerCAPEM = issued.CACertificatePEM
	}
	if len(issued.CertificatePEM) == 0 || len(issued.CACertificatePEM) == 0 || len(issued.ServerCAPEM) == 0 || issued.Serial == "" || len(issued.Fingerprint) == 0 || !issued.NotAfter.After(now) {
		return errors.New("issued agent certificate is incomplete")
	}
	return nil
}

func insertAgentCredential(ctx context.Context, tx pgx.Tx, installationID int64, status, attemptID string, csrFingerprint []byte, issued AgentCertificate, now time.Time) error {
	serverCAPEM := issued.ServerCAPEM
	if len(serverCAPEM) == 0 {
		serverCAPEM = issued.CACertificatePEM
	}
	_, err := tx.Exec(ctx, `INSERT INTO agent_credentials(installation_id,status,enrollment_attempt_id,csr_fingerprint,certificate_pem,
		ca_certificate_pem,server_ca_certificate_pem,certificate_serial,certificate_fingerprint,certificate_not_after,created_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11)`, installationID, status, attemptID, csrFingerprint, issued.CertificatePEM,
		issued.CACertificatePEM, serverCAPEM, issued.Serial, issued.Fingerprint, issued.NotAfter, now)
	return err
}

func credentialForAttempt(ctx context.Context, query rowQuerier, installationID int64, attemptID string, csrFingerprint []byte) (AgentCertificate, bool, error) {
	var value AgentCertificate
	var storedCSR []byte
	err := query.QueryRow(ctx, `SELECT c.certificate_pem,c.ca_certificate_pem,c.server_ca_certificate_pem,c.certificate_serial,c.certificate_fingerprint,c.certificate_not_after,c.csr_fingerprint
		FROM agent_credentials c
		WHERE c.installation_id=$1 AND c.enrollment_attempt_id=$2`, installationID, attemptID).
		Scan(&value.CertificatePEM, &value.CACertificatePEM, &value.ServerCAPEM, &value.Serial, &value.Fingerprint, &value.NotAfter, &storedCSR)
	if errors.Is(err, pgx.ErrNoRows) || !bytes.Equal(storedCSR, csrFingerprint) {
		return AgentCertificate{}, false, nil
	}
	return value, err == nil, err
}

func validCredential(ctx context.Context, query rowQuerier, installationID int64, fingerprint []byte, now time.Time) (int64, error) {
	var credentialID int64
	var status string
	var notAfter time.Time
	var overlapNotAfter *time.Time
	err := query.QueryRow(ctx, `SELECT id,status,certificate_not_after,overlap_not_after FROM agent_credentials
		WHERE installation_id=$1 AND certificate_fingerprint=$2`, installationID, fingerprint).
		Scan(&credentialID, &status, &notAfter, &overlapNotAfter)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrAgentIdentityMismatch
	}
	if err != nil {
		return 0, err
	}
	if !notAfter.After(now) || status == "Revoked" {
		return 0, ErrAgentIdentityMismatch
	}
	if status == "Superseded" && (overlapNotAfter == nil || !overlapNotAfter.After(now)) {
		return 0, ErrAgentIdentityMismatch
	}
	if status != "Active" && status != "Superseded" {
		return 0, ErrAgentIdentityMismatch
	}
	return credentialID, nil
}

func (s *Store) ActivateAgent(ctx context.Context, publicID string, fingerprint []byte, clusterUID, agentVersion, kubernetesVersion string, capabilities []string, now time.Time, event audit.Event) (bool, error) {
	if strings.TrimSpace(clusterUID) == "" || strings.TrimSpace(agentVersion) == "" || strings.TrimSpace(kubernetesVersion) == "" || len(capabilities) == 0 {
		return false, ErrAgentIdentityMismatch
	}
	capabilitiesJSON, err := json.Marshal(capabilities)
	if err != nil {
		return false, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	var installationID int64
	var status string
	var storedClusterUID *string
	if err = tx.QueryRow(ctx, `SELECT id,status,cluster_uid FROM agent_installations WHERE public_id=$1 FOR UPDATE`, publicID).Scan(&installationID, &status, &storedClusterUID); errors.Is(err, pgx.ErrNoRows) {
		return false, ErrAgentIdentityMismatch
	}
	if err != nil {
		return false, err
	}
	if status == "Revoked" || (storedClusterUID != nil && *storedClusterUID != clusterUID) {
		return false, ErrAgentIdentityMismatch
	}
	if _, err = validCredential(ctx, tx, installationID, fingerprint, now); err != nil {
		return false, err
	}
	first := status == "Pending"
	if _, err = tx.Exec(ctx, `UPDATE agent_installations SET status='Active',cluster_uid=$2,agent_version=$3,kubernetes_version=$4,
		capabilities_json=$5,last_seen_at=$6,updated_at=$6 WHERE id=$1`, installationID, clusterUID, agentVersion, kubernetesVersion, capabilitiesJSON, now); err != nil {
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
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var installationID int64
	var status string
	if err = tx.QueryRow(ctx, `SELECT id,status FROM agent_installations WHERE public_id=$1 FOR UPDATE`, publicID).Scan(&installationID, &status); errors.Is(err, pgx.ErrNoRows) || status != "Active" {
		return ErrAgentIdentityMismatch
	}
	if err != nil {
		return err
	}
	if _, err = validCredential(ctx, tx, installationID, fingerprint, now); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE agent_installations SET last_seen_at=$2,updated_at=$2 WHERE id=$1`, installationID, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ListClusters(ctx context.Context) ([]AgentInstallation, error) {
	rows, err := s.Pool.Query(ctx, clusterSelect+` ORDER BY i.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	clusters := make([]AgentInstallation, 0)
	for rows.Next() {
		value, scanErr := scanCluster(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		clusters = append(clusters, value)
	}
	return clusters, rows.Err()
}

const clusterSelect = `SELECT i.id,i.public_id,i.name,i.status,COALESCE(i.cluster_uid,''),i.agent_version,i.kubernetes_version,
	i.capabilities_json::text,c.certificate_not_after,i.last_seen_at,i.revoked_at,i.revocation_reason,i.created_at,i.updated_at
	FROM agent_installations i LEFT JOIN agent_credentials c ON c.installation_id=i.id AND c.status='Active'`

func (s *Store) FindCluster(ctx context.Context, publicID string) (AgentInstallation, error) {
	value, err := scanCluster(s.Pool.QueryRow(ctx, clusterSelect+` WHERE i.public_id=$1`, publicID))
	if errors.Is(err, pgx.ErrNoRows) {
		return AgentInstallation{}, ErrClusterNotFound
	}
	return value, err
}

type clusterScanner interface{ Scan(...any) error }

func scanCluster(row clusterScanner) (AgentInstallation, error) {
	var value AgentInstallation
	var capabilities []byte
	err := row.Scan(&value.ID, &value.PublicID, &value.Name, &value.Status, &value.ClusterUID, &value.AgentVersion,
		&value.KubernetesVersion, &capabilities, &value.CertificateExpiry, &value.LastSeenAt, &value.RevokedAt,
		&value.RevocationReason, &value.CreatedAt, &value.UpdatedAt)
	if err == nil {
		err = json.Unmarshal(capabilities, &value.Capabilities)
	}
	return value, err
}

func (s *Store) ReissueClusterEnrollment(ctx context.Context, publicID string, tokenHash []byte, expiresAt time.Time, event audit.Event) error {
	if len(tokenHash) == 0 || !expiresAt.After(time.Now()) {
		return ErrAgentEnrollmentInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var installationID int64
	var status string
	if err = tx.QueryRow(ctx, `SELECT id,status FROM agent_installations WHERE public_id=$1 FOR UPDATE`, publicID).Scan(&installationID, &status); errors.Is(err, pgx.ErrNoRows) {
		return ErrClusterNotFound
	}
	if err != nil {
		return err
	}
	if status != "Pending" {
		return ErrClusterNotPending
	}
	if _, err = tx.Exec(ctx, `INSERT INTO agent_enrollment_tokens(installation_id,token_hash,expires_at,consumed_at)
		VALUES($1,$2,$3,NULL) ON CONFLICT(installation_id) DO UPDATE SET token_hash=EXCLUDED.token_hash,
		expires_at=EXCLUDED.expires_at,consumed_at=NULL,created_at=now()`, installationID, tokenHash, expiresAt); err != nil {
		return err
	}
	event.TargetPublicID = publicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) RevokeCluster(ctx context.Context, publicID, reason string, now time.Time, event audit.Event) error {
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 500 {
		return ErrAgentEnrollmentInvalid
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var installationID int64
	if err = tx.QueryRow(ctx, `UPDATE agent_installations SET status='Revoked',revoked_at=$2,revocation_reason=$3,updated_at=$2
		WHERE public_id=$1 AND status<>'Revoked' RETURNING id`, publicID, now, reason).Scan(&installationID); errors.Is(err, pgx.ErrNoRows) {
		return ErrClusterNotFound
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE agent_credentials SET status='Revoked',overlap_not_after=NULL,updated_at=$2 WHERE installation_id=$1`, installationID, now); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE operations SET status='Failed',completed_at=$2,lease_until=NULL,worker_id=NULL,
		error_code='cluster_revoked',error_message='target cluster was revoked',updated_at=$2
		WHERE agent_installation_id=$1 AND status IN ('Pending','Running')`, installationID, now); err != nil {
		return err
	}
	event.TargetPublicID = publicID
	if err = insertAudit(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
