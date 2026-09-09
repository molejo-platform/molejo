package workspaceprovisioning

import "testing"

func TestDecideSeparatesProvisioningGates(t *testing.T) {
	tests := []struct {
		name   string
		input  Input
		accept bool
		reason Reason
	}{
		{name: "accepted", input: allowedInput(), accept: true, reason: ReasonAccepted},
		{name: "actor denied", input: with(allowedInput(), func(input *Input) { input.ActorAuthorized = false }), reason: ReasonActorUnauthorized},
		{name: "cluster detached", input: with(allowedInput(), func(input *Input) { input.ClusterAttached = false }), reason: ReasonClusterNotAttached},
		{name: "capability missing", input: with(allowedInput(), func(input *Input) { input.CapabilityAvailable = false }), reason: ReasonCapabilityUnavailable},
		{name: "operator disabled", input: with(allowedInput(), func(input *Input) { input.Consent = ConsentDisabled }), reason: ReasonConsentDisabled},
		{name: "reserved namespace", input: with(allowedInput(), func(input *Input) { input.Namespace = "kube-system" }), reason: ReasonNamespaceReserved},
		{name: "invalid namespace", input: with(allowedInput(), func(input *Input) { input.Namespace = "Not_A_Name" }), reason: ReasonNamespaceInvalid},
		{name: "idempotency missing", input: with(allowedInput(), func(input *Input) { input.IdempotencyPresent = false }), reason: ReasonIdempotencyRequired},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := Decide(test.input)
			if decision.Accepted != test.accept || decision.Reason != test.reason {
				t.Fatalf("decision=%+v, want accepted=%t reason=%s", decision, test.accept, test.reason)
			}
		})
	}
}

func TestParseConsentRejectsUnknownModes(t *testing.T) {
	for _, test := range []struct {
		value string
		want  Consent
		ok    bool
	}{
		{value: "", want: ConsentDisabled, ok: true},
		{value: "Disabled", want: ConsentDisabled, ok: true},
		{value: "Namespaced", want: ConsentNamespaced, ok: true},
		{value: "enabled", ok: false},
	} {
		got, ok := ParseConsent(test.value)
		if got != test.want || ok != test.ok {
			t.Fatalf("ParseConsent(%q)=(%q,%t), want (%q,%t)", test.value, got, ok, test.want, test.ok)
		}
	}
}

func allowedInput() Input {
	return Input{ActorAuthorized: true, ClusterAttached: true, CapabilityAvailable: true, Consent: ConsentNamespaced, Namespace: "ws-abcdefghijklmnopqrst", IdempotencyPresent: true}
}

func with(input Input, change func(*Input)) Input {
	change(&input)
	return input
}
