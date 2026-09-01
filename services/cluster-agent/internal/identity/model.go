package identity

import "time"

type StoredIdentity struct {
	AttemptID        string
	PrivateKeyPEM    []byte
	CSRPEM           []byte
	InstallationID   string
	CertificatePEM   []byte
	CACertificatePEM []byte
	ExpiresAt        time.Time
}

type Certificate struct {
	InstallationID   string
	CertificatePEM   []byte
	CACertificatePEM []byte
	ExpiresAt        time.Time
}
