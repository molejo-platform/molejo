package api

import (
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func TestValidTOTPStepAcceptsClockSkewAndIdentifiesTheCounter(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "Molejo", AccountName: "test.user", Secret: []byte("01234567890123456789")})
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCode(key.Secret(), now.Add(-30*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	step, valid := validTOTPStep(key.Secret(), code, now)
	if !valid || step != now.Add(-30*time.Second).Unix()/30 {
		t.Fatalf("step=%d valid=%v", step, valid)
	}
	if _, valid = validTOTPStep(key.Secret(), "000000", now); valid {
		t.Fatal("invalid TOTP was accepted")
	}
}

func TestEnrollmentSecretRejectsMalformedChallengePayload(t *testing.T) {
	if _, _, ok := enrollmentSecret(map[string]any{"secretReference": "identity/totp/usr", "secretVersion": 1.5}); ok {
		t.Fatal("fractional secret version was accepted")
	}
	if reference, version, ok := enrollmentSecret(map[string]any{"secretReference": "identity/totp/usr", "secretVersion": float64(2)}); !ok || reference == "" || version != 2 {
		t.Fatalf("reference=%q version=%d ok=%v", reference, version, ok)
	}
}
