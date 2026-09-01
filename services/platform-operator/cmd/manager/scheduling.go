package main

import (
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const statefulTolerationsEnvironment = "MOLEJO_STATEFUL_TOLERATIONS_JSON"

func parseStatefulTolerations(raw string) ([]corev1.Toleration, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	var tolerations []corev1.Toleration
	if err := json.Unmarshal([]byte(raw), &tolerations); err != nil {
		return nil, fmt.Errorf("decode %s: %w", statefulTolerationsEnvironment, err)
	}
	if tolerations == nil {
		return nil, fmt.Errorf("%s must be a JSON array", statefulTolerationsEnvironment)
	}
	for index := range tolerations {
		toleration := &tolerations[index]
		if toleration.Operator == "" {
			toleration.Operator = corev1.TolerationOpEqual
		}
		if strings.TrimSpace(toleration.Key) == "" {
			return nil, fmt.Errorf("%s[%d].key is required", statefulTolerationsEnvironment, index)
		}
		switch toleration.Operator {
		case corev1.TolerationOpEqual:
		case corev1.TolerationOpExists:
			if toleration.Value != "" {
				return nil, fmt.Errorf("%s[%d].value must be empty with Exists", statefulTolerationsEnvironment, index)
			}
		default:
			return nil, fmt.Errorf("%s[%d].operator is unsupported", statefulTolerationsEnvironment, index)
		}
		switch toleration.Effect {
		case "", corev1.TaintEffectNoSchedule, corev1.TaintEffectPreferNoSchedule, corev1.TaintEffectNoExecute:
		default:
			return nil, fmt.Errorf("%s[%d].effect is unsupported", statefulTolerationsEnvironment, index)
		}
	}
	return tolerations, nil
}
