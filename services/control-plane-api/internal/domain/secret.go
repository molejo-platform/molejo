package domain

// These internal types prevent backend identity, delivery identity, and
// plaintext values from being passed interchangeably inside the control plane.
type (
	SecretReference       string
	SecretBackendVersion  int64
	SecretFingerprint     []byte
	SecretDeliveryVersion int64
)
