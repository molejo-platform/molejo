package automation

import (
	"strings"
	"testing"
	"time"
)

func TestValidateEnvironmentScope(t *testing.T) {
	tests := []struct {
		name    string
		values  []string
		wantErr bool
	}{
		{name: "empty", values: []string{}},
		{name: "one", values: environmentIDs(1)},
		{name: "twenty", values: environmentIDs(20)},
		{name: "twenty one", values: environmentIDs(21), wantErr: true},
		{name: "invalid ID", values: []string{"aev-invalid"}, wantErr: true},
		{name: "duplicate", values: []string{environmentIDs(1)[0], environmentIDs(1)[0]}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateEnvironmentScope(test.values)
			if (err != nil) != test.wantErr {
				t.Fatalf("ValidateEnvironmentScope() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestResolveCredentialExpiry(t *testing.T) {
	now := time.Date(2026, time.September, 5, 12, 0, 0, 0, time.FixedZone("test", -3*60*60))
	minimum := now.Add(time.Minute)
	maximum := now.Add(365 * 24 * time.Hour)
	beforeMinimum := minimum.Add(-time.Nanosecond)
	afterMaximum := maximum.Add(time.Nanosecond)
	requestedWithOffset := now.Add(time.Hour)

	tests := []struct {
		name      string
		requested *time.Time
		want      time.Time
		wantErr   bool
	}{
		{name: "default", want: now.Add(90 * 24 * time.Hour).UTC()},
		{name: "minimum inclusive", requested: &minimum, want: minimum.UTC()},
		{name: "maximum inclusive", requested: &maximum, want: maximum.UTC()},
		{name: "normalizes UTC", requested: &requestedWithOffset, want: requestedWithOffset.UTC()},
		{name: "before minimum", requested: &beforeMinimum, wantErr: true},
		{name: "after maximum", requested: &afterMaximum, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolveCredentialExpiry(now, test.requested)
			if (err != nil) != test.wantErr {
				t.Fatalf("ResolveCredentialExpiry() error = %v, wantErr %v", err, test.wantErr)
			}
			if !test.wantErr && !got.Equal(test.want) {
				t.Fatalf("ResolveCredentialExpiry() = %s, want %s", got, test.want)
			}
		})
	}
}

func environmentIDs(count int) []string {
	values := make([]string, 0, count)
	for index := range count {
		values = append(values, "aev-"+strings.Repeat(string(rune('a'+index)), 20))
	}
	return values
}
