// Package secretdelivery owns the last-mile mechanism used to deliver secret
// values to an application runtime. Backend custody is a separate concern.
package secretdelivery

import "errors"

type Mode string

const MaterializedKubernetesSecret Mode = "MaterializedKubernetesSecret"

func Validate(mode Mode) error {
	if mode != MaterializedKubernetesSecret {
		return errors.New("secret delivery mode is unsupported")
	}
	return nil
}
