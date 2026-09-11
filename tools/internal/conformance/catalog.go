package conformance

import (
	"fmt"
	"sort"
	"time"
)

func Profiles() []Profile {
	profiles := []Profile{alphaCoreProfile()}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].ID < profiles[j].ID })
	return profiles
}

func ProfileByID(id string) (Profile, error) {
	for _, profile := range Profiles() {
		if profile.ID == id {
			return profile, nil
		}
	}
	return Profile{}, fmt.Errorf("unknown conformance profile %q", id)
}

func alphaCoreProfile() Profile {
	return Profile{
		ID: AlphaCoreProfileID, Version: AlphaCoreVersion,
		Description: "Molejo authentication, application lifecycle, observability, withdrawal, and cleanup",
		Scenarios: []Scenario{{
			ID: "application-lifecycle", Description: "deploy and withdraw one immutable stateless application",
			Required: true, Timeout: 8 * time.Minute, Run: runApplicationLifecycle,
		}},
	}
}
