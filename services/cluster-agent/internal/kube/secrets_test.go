package kube

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestSecretStorePersistsIdentityAndClearsEnrollmentToken(t *testing.T) {
	client := fake.NewClientset(
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "molejo-agent-identity", Namespace: "molejo-system"}},
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "molejo-agent-enrollment", Namespace: "molejo-system"}, Data: map[string][]byte{"token": []byte("one-time")}},
	)
	store := NewSecretStore(client.CoreV1(), "molejo-system", "molejo-agent-identity", "molejo-agent-enrollment")
	identity := Identity{AttemptID: "ena-test", PrivateKeyPEM: []byte("private"), CSRPEM: []byte("csr")}
	if err := store.SaveEnrollmentIdentity(t.Context(), identity); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadIdentity(t.Context())
	if err != nil || loaded.AttemptID != identity.AttemptID || string(loaded.PrivateKeyPEM) != "private" {
		t.Fatalf("loaded=%+v err=%v", loaded, err)
	}
	token, err := store.EnrollmentToken(t.Context())
	if err != nil || token != "one-time" {
		t.Fatalf("token=%q err=%v", token, err)
	}
	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	if err = store.SaveCertificate(t.Context(), Certificate{InstallationID: "agi-test", CertificatePEM: []byte("certificate"), CACertificatePEM: []byte("ca"), ExpiresAt: expiresAt}); err != nil {
		t.Fatal(err)
	}
	if err = store.ClearEnrollmentToken(t.Context()); err != nil {
		t.Fatal(err)
	}
	token, err = store.EnrollmentToken(t.Context())
	if err != nil || token != "" {
		t.Fatalf("cleared token=%q err=%v", token, err)
	}
	loaded, err = store.LoadIdentity(t.Context())
	if err != nil || loaded.InstallationID != "agi-test" || !loaded.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("certificate identity=%+v err=%v", loaded, err)
	}
}
