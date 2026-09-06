package store

import (
	"os"
	"testing"

	"github.com/molejo-platform/molejo/services/control-plane-api/internal/testsupport"
)

func TestMain(m *testing.M) {
	os.Exit(testsupport.RunPostgresTests(m.Run))
}
