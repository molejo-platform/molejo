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
	Enroll(context.Context, string, string, []byte) (agentidentity.Certificate, error)
}

type Connector interface {
	Connect(context.Context, agentidentity.StoredIdentity, func()) error
}

type Runner struct {
	Store               IdentityStore
	Enroller            Enroller
	Connector           Connector
	Status              *Status
	ValidateCertificate func(agentidentity.StoredIdentity, agentidentity.Certificate, time.Time) error
	now                 func() time.Time
	backoff             *Backoff
}

func NewRunner(store IdentityStore, enroller Enroller, connector Connector, status *Status) *Runner {
	if status == nil {
		status = NewStatus()
	}
	return &Runner{Store: store, Enroller: enroller, Connector: connector, Status: status, ValidateCertificate: ValidateCertificate, now: func() time.Time { return time.Now().UTC() }, backoff: NewBackoff(uint64(time.Now().UnixNano()))}
}

func (r *Runner) Run(ctx context.Context) {
	for ctx.Err() == nil {
		err := r.ReconcileOnce(ctx)
		if ctx.Err() != nil {
			return
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
			return
		case <-timer.C:
		}
	}
}

func (r *Runner) ReconcileOnce(ctx context.Context) error {
	if r.Store == nil {
		return r.fail("identity storage is unavailable", errors.New("identity storage is unavailable"))
	}
	stored, err := r.Store.LoadIdentity(ctx)
	if err != nil {
		return r.fail("identity Secret is not readable", err)
	}
	if len(stored.CertificatePEM) == 0 {
		stored, err = r.ensureEnrollmentIdentity(ctx, stored)
		if err != nil {
			return r.fail("identity Secret is not writable", err)
		}
		token, tokenErr := r.Store.EnrollmentToken(ctx)
		if tokenErr != nil {
			return r.fail("enrollment Secret is not readable", tokenErr)
		}
		if token == "" {
			r.Status.Set(StateUnpaired, "waiting for an enrollment token")
			return nil
		}
		if r.Enroller == nil {
			r.Status.Set(StateUnconfigured, "enrollment endpoint is not configured")
			return nil
		}
		r.Status.Set(StateEnrolling, "")
		certificate, enrollErr := r.Enroller.Enroll(ctx, token, stored.AttemptID, stored.CSRPEM)
		if enrollErr != nil {
			r.Status.Set(StateUnpaired, "enrollment has not completed")
			return enrollErr
		}
		if err = r.ValidateCertificate(stored, certificate, r.now()); err != nil {
			return r.fail("enrollment returned an invalid identity", err)
		}
		if err = r.Store.SaveCertificate(ctx, certificate); err != nil {
			return r.fail("identity Secret is not writable", err)
		}
		stored.InstallationID, stored.CertificatePEM, stored.CACertificatePEM, stored.ExpiresAt = certificate.InstallationID, certificate.CertificatePEM, certificate.CACertificatePEM, certificate.ExpiresAt
	}
	if err = r.clearEnrollmentToken(ctx); err != nil {
		return r.fail("enrollment token could not be cleared", err)
	}
	if r.Connector == nil {
		r.Status.Set(StateUnconfigured, "gRPC endpoint is not configured")
		return nil
	}
	if stored.ExpiresAt.IsZero() || !stored.ExpiresAt.After(r.now()) {
		return r.fail("Agent certificate is expired", errors.New("Agent certificate is expired"))
	}
	r.Status.Set(StateConnecting, "")
	err = r.Connector.Connect(ctx, stored, func() { r.Status.Set(StatePaired, "") })
	if err != nil && ctx.Err() == nil {
		r.Status.Set(StateConnecting, "connection interrupted")
		return err
	}
	return err
}

func (r *Runner) clearEnrollmentToken(ctx context.Context) error {
	token, err := r.Store.EnrollmentToken(ctx)
	if err != nil || token == "" {
		return err
	}
	return r.Store.ClearEnrollmentToken(ctx)
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
	if err = r.Store.SaveEnrollmentIdentity(ctx, stored); err != nil {
		return agentidentity.StoredIdentity{}, err
	}
	return stored, nil
}

func (r *Runner) fail(reason string, err error) error {
	r.Status.Set(StateFailed, reason)
	return fmt.Errorf("%s: %w", reason, err)
}
