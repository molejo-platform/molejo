package kube

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/util/retry"

	agentidentity "github.com/molejo-platform/molejo/services/cluster-agent/internal/identity"
)

type (
	Identity    = agentidentity.StoredIdentity
	Certificate = agentidentity.Certificate
)

type SecretStore struct {
	core           v1.CoreV1Interface
	namespace      string
	identityName   string
	enrollmentName string
}

func NewSecretStore(core v1.CoreV1Interface, namespace, identityName, enrollmentName string) *SecretStore {
	return &SecretStore{core: core, namespace: namespace, identityName: identityName, enrollmentName: enrollmentName}
}

func (s *SecretStore) LoadIdentity(ctx context.Context) (Identity, error) {
	secret, err := s.core.Secrets(s.namespace).Get(ctx, s.identityName, metav1.GetOptions{})
	if err != nil {
		return Identity{}, fmt.Errorf("read Agent identity Secret: %w", err)
	}
	value := Identity{
		AttemptID: string(secret.Data["enrollment-attempt-id"]), PrivateKeyPEM: append([]byte(nil), secret.Data["private-key.pem"]...), CSRPEM: append([]byte(nil), secret.Data["csr.pem"]...),
		InstallationID: string(secret.Data["installation-id"]), CertificatePEM: append([]byte(nil), secret.Data["tls.crt"]...), CACertificatePEM: append([]byte(nil), secret.Data["ca.crt"]...),
		ServerCAPEM: append([]byte(nil), secret.Data["server-ca.crt"]...), TrustBundleID: string(secret.Data["trust-bundle-id"]), RenewalAttemptID: string(secret.Data["renewal-attempt-id"]),
		RenewalKeyPEM: append([]byte(nil), secret.Data["renewal-private-key.pem"]...), RenewalCSRPEM: append([]byte(nil), secret.Data["renewal-csr.pem"]...),
	}
	if len(value.ServerCAPEM) == 0 {
		value.ServerCAPEM = append([]byte(nil), value.CACertificatePEM...)
	}
	if raw := strings.TrimSpace(string(secret.Data["certificate-not-after"])); raw != "" {
		value.ExpiresAt, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return Identity{}, fmt.Errorf("Agent certificate expiry is invalid")
		}
	}
	return value, nil
}

func (s *SecretStore) SaveEnrollmentIdentity(ctx context.Context, value Identity) error {
	if value.AttemptID == "" || len(value.PrivateKeyPEM) == 0 || len(value.CSRPEM) == 0 {
		return errors.New("Agent enrollment identity is incomplete")
	}
	return s.update(ctx, s.identityName, func(secret *corev1.Secret) {
		secret.Data["enrollment-attempt-id"] = []byte(value.AttemptID)
		secret.Data["private-key.pem"] = append([]byte(nil), value.PrivateKeyPEM...)
		secret.Data["csr.pem"] = append([]byte(nil), value.CSRPEM...)
	})
}

func (s *SecretStore) SaveCertificate(ctx context.Context, value Certificate) error {
	if value.InstallationID == "" || len(value.CertificatePEM) == 0 || len(value.CACertificatePEM) == 0 || value.TrustBundleID == "" || value.ExpiresAt.IsZero() {
		return errors.New("Agent certificate is incomplete")
	}
	return s.update(ctx, s.identityName, func(secret *corev1.Secret) {
		secret.Data["installation-id"] = []byte(value.InstallationID)
		if len(value.PrivateKeyPEM) != 0 {
			secret.Data["private-key.pem"] = append([]byte(nil), value.PrivateKeyPEM...)
		}
		secret.Data["tls.crt"] = append([]byte(nil), value.CertificatePEM...)
		secret.Data["ca.crt"] = append([]byte(nil), value.CACertificatePEM...)
		serverCA := value.ServerCAPEM
		if len(serverCA) == 0 {
			serverCA = value.CACertificatePEM
		}
		secret.Data["server-ca.crt"] = append([]byte(nil), serverCA...)
		secret.Data["trust-bundle-id"] = []byte(value.TrustBundleID)
		secret.Data["certificate-not-after"] = []byte(value.ExpiresAt.UTC().Format(time.RFC3339Nano))
		delete(secret.Data, "renewal-attempt-id")
		delete(secret.Data, "renewal-private-key.pem")
		delete(secret.Data, "renewal-csr.pem")
	})
}

func (s *SecretStore) SaveRenewalIdentity(ctx context.Context, value Identity) error {
	if value.RenewalAttemptID == "" || len(value.RenewalKeyPEM) == 0 || len(value.RenewalCSRPEM) == 0 {
		return errors.New("Agent renewal identity is incomplete")
	}
	return s.update(ctx, s.identityName, func(secret *corev1.Secret) {
		secret.Data["renewal-attempt-id"] = []byte(value.RenewalAttemptID)
		secret.Data["renewal-private-key.pem"] = append([]byte(nil), value.RenewalKeyPEM...)
		secret.Data["renewal-csr.pem"] = append([]byte(nil), value.RenewalCSRPEM...)
	})
}

func (s *SecretStore) EnrollmentToken(ctx context.Context) (string, error) {
	secret, err := s.core.Secrets(s.namespace).Get(ctx, s.enrollmentName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read Agent enrollment Secret: %w", err)
	}
	return strings.TrimSpace(string(secret.Data["token"])), nil
}

func (s *SecretStore) ClearEnrollmentToken(ctx context.Context) error {
	err := s.update(ctx, s.enrollmentName, func(secret *corev1.Secret) { delete(secret.Data, "token") })
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func (s *SecretStore) update(ctx context.Context, name string, mutate func(*corev1.Secret)) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		secret, err := s.core.Secrets(s.namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if secret.Data == nil {
			secret.Data = map[string][]byte{}
		}
		mutate(secret)
		_, err = s.core.Secrets(s.namespace).Update(ctx, secret, metav1.UpdateOptions{})
		return err
	})
}
