package identity

import "time"

type StoredIdentity struct {
	AttemptID        string
	PrivateKeyPEM    []byte
	CSRPEM           []byte
	InstallationID   string
	CertificatePEM   []byte
	CACertificatePEM []byte
	ServerCAPEM      []byte
	ExpiresAt        time.Time
	RenewalAttemptID string
	RenewalKeyPEM    []byte
	RenewalCSRPEM    []byte
}

type Certificate struct {
	InstallationID   string
	PrivateKeyPEM    []byte
	CertificatePEM   []byte
	CACertificatePEM []byte
	ServerCAPEM      []byte
	ExpiresAt        time.Time
}
