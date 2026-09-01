package parameters

import (
	"context"
	"errors"
)

var (
	ErrUnavailable = errors.New("secret store unavailable")
	ErrConflict    = errors.New("secret version conflict")
)

// SecretValueStore is declared by the parameter domain that consumes it. Its
// references and versions are internal implementation details, never API IDs.
type SecretValueStore interface {
	Put(context.Context, string, string, int64) (int64, error)
	Get(context.Context, string, int64) (string, error)
	CurrentVersion(context.Context, string) (int64, error)
	Delete(context.Context, string) error
}

type UnavailableStore struct{}

func (UnavailableStore) Put(context.Context, string, string, int64) (int64, error) {
	return 0, ErrUnavailable
}

func (UnavailableStore) Get(context.Context, string, int64) (string, error) {
	return "", ErrUnavailable
}

func (UnavailableStore) CurrentVersion(context.Context, string) (int64, error) {
	return 0, ErrUnavailable
}

func (UnavailableStore) Delete(context.Context, string) error { return ErrUnavailable }
