package main

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestParseStatefulTolerations(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []corev1.Toleration
		wantErr bool
	}{
		{name: "not configured"},
		{
			name: "one installation toleration",
			raw:  `[{"key":"platform.example.com/workload","operator":"Equal","value":"data","effect":"NoSchedule"}]`,
			want: []corev1.Toleration{{
				Key:      "platform.example.com/workload",
				Operator: corev1.TolerationOpEqual,
				Value:    "data",
				Effect:   corev1.TaintEffectNoSchedule,
			}},
		},
		{name: "malformed JSON", raw: `[`, wantErr: true},
		{name: "unsupported effect", raw: `[{"key":"example.com/workload","effect":"Unsupported"}]`, wantErr: true},
		{name: "equal requires key", raw: `[{"operator":"Equal","value":"data","effect":"NoSchedule"}]`, wantErr: true},
		{name: "exists forbids value", raw: `[{"key":"example.com/workload","operator":"Exists","value":"data","effect":"NoSchedule"}]`, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseStatefulTolerations(test.raw)
			if test.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %#v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse stateful tolerations: %v", err)
			}
			if len(got) != len(test.want) {
				t.Fatalf("tolerations = %#v", got)
			}
			for index := range got {
				if got[index] != test.want[index] {
					t.Fatalf("toleration[%d] = %#v", index, got[index])
				}
			}
		})
	}
}
