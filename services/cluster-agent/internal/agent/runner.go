package agent

import (
	"context"
	"errors"
	"fmt"
	"time"

	agentidentity "github.com/molejo-platform/molejo/services/cluster-agent/internal/identity"
)

type IdentityStore interface {
	LoadIdentity(context.Context) (agentidentity.StoredIdentity, error)
	SaveEnrollmentIdentity(context.Context, agentidentity.StoredIdentity) error
	EnrollmentToken(context.Context) (string, error)
	SaveCertificate(context.Context, agentidentity.Certificate) error
	ClearEnrollmentToken(context.Context) error
}

type Enroller interface {
	Enroll(context.Context, EnrollmentRequest) (agentidentity.Certificate, error)
}

type EnrollmentRequest struct {
	Token     string
	AttemptID string
	CSRPEM    []byte
}

type Connector interface {
	Connect(context.Context, agentidentity.StoredIdentity, func()) error
}

type Runner struct {
	store               IdentityStore
	enroller            Enroller
	connector           Connector
	status              *Status
	validateCertificate func(agentidentity.StoredIdentity, agentidentity.Certificate, time.Time) error
	now                 func() time.Time
	backoff             *Backoff
}

func NewRunner(store IdentityStore, enroller Enroller, connector Connector, status *Status) *Runner {
	if status == nil {
		status = NewStatus()
	}
	return &Runner{store: store, enroller: enroller, connector: connector, status: status, validateCertificate: ValidateCertificate, now: func() time.Time { return time.Now().UTC() }, backoff: NewBackoff(uint64(time.Now().UnixNano()))}
}

func (r *Runner) Run(ctx context.Context) error {
	for ctx.Err() == nil {
		err := r.ReconcileOnce(ctx)
		if ctx.Err() != nil {
			return nil
		}
		delay := 5 * time.Second
		if err != nil {
			delay = r.backoff.Next()
		} else {
			r.backoff.Reset()
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
	return nil
}

func (r *Runner) ReconcileOnce(ctx context.Context) error {
	if r.store == nil {
		return r.fail("identity storage is unavailable", errors.New("identity storage is unavailable"))
	}
	stored, err := r.store.LoadIdentity(ctx)
	if err != nil {
		return r.fail("identity Secret is not readable", err)
	}
	if len(stored.CertificatePEM) == 0 {
		stored, err = r.ensureEnrollmentIdentity(ctx, stored)
		if err != nil {
			return r.fail("identity Secret is not writable", err)
		}
		token, tokenErr := r.store.EnrollmentToken(ctx)
		if tokenErr != nil {
			return r.fail("enrollment Secret is not readable", tokenErr)
		}
		if token == "" {
			r.status.Set(StateUnpaired, "waiting for an enrollment token")
			return nil
		}
		if r.enroller == nil {
			r.status.Set(StateUnconfigured, "enrollment endpoint is not configured")
			return nil
		}
		r.status.Set(StateEnrolling, "")
		certificate, enrollErr := r.enroller.Enroll(ctx, EnrollmentRequest{Token: token, AttemptID: stored.AttemptID, CSRPEM: stored.CSRPEM})
		if enrollErr != nil {
			r.status.Set(StateUnpaired, "enrollment has not completed")
			return enrollErr
		}
		if err = r.validateCertificate(stored, certificate, r.now()); err != nil {
			return r.fail("enrollment returned an invalid identity", err)
		}
		if err = r.store.SaveCertificate(ctx, certificate); err != nil {
			return r.fail("identity Secret is not writable", err)
		}
		stored.InstallationID, stored.CertificatePEM, stored.CACertificatePEM, stored.ExpiresAt = certificate.InstallationID, certificate.CertificatePEM, certificate.CACertificatePEM, certificate.ExpiresAt
	}
	if err = r.clearEnrollmentToken(ctx); err != nil {
		return r.fail("enrollment token could not be cleared", err)
	}
	if r.connector == nil {
		r.status.Set(StateUnconfigured, "gRPC endpoint is not configured")
		return nil
	}
	if stored.ExpiresAt.IsZero() || !stored.ExpiresAt.After(r.now()) {
		return r.fail("Agent certificate is expired", errors.New("Agent certificate is expired"))
	}
	r.status.Set(StateConnecting, "")
	err = r.connector.Connect(ctx, stored, func() { r.status.Set(StatePaired, "") })
	if err != nil && ctx.Err() == nil {
		r.status.Set(StateConnecting, "connection interrupted")
		return err
	}
	return err
}

func (r *Runner) clearEnrollmentToken(ctx context.Context) error {
	token, err := r.store.EnrollmentToken(ctx)
	if err != nil || token == "" {
		return err
	}
	return r.store.ClearEnrollmentToken(ctx)
}

func (r *Runner) ensureEnrollmentIdentity(ctx context.Context, stored agentidentity.StoredIdentity) (agentidentity.StoredIdentity, error) {
	complete := stored.AttemptID != "" && len(stored.PrivateKeyPEM) != 0 && len(stored.CSRPEM) != 0
	empty := stored.AttemptID == "" && len(stored.PrivateKeyPEM) == 0 && len(stored.CSRPEM) == 0
	if complete {
		return stored, nil
	}
	if !empty {
		return agentidentity.StoredIdentity{}, errors.New("persisted enrollment identity is incomplete")
	}
	created, err := agentidentity.NewEnrollmentIdentity()
	if err != nil {
		return agentidentity.StoredIdentity{}, err
	}
	stored.AttemptID, stored.PrivateKeyPEM, stored.CSRPEM = created.AttemptID, created.PrivateKeyPEM, created.CSRPEM
	if err = r.store.SaveEnrollmentIdentity(ctx, stored); err != nil {
		return agentidentity.StoredIdentity{}, err
	}
	return stored, nil
}

func (r *Runner) fail(reason string, err error) error {
	r.status.Set(StateFailed, reason)
	return fmt.Errorf("%s: %w", reason, err)
}
