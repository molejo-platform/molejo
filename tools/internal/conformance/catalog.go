package conformance

import (
	"fmt"
	"sort"
	"time"
)

func Profiles() []Profile {
	profiles := []Profile{alphaCoreProfile(), httpPublicationProfile()}
	sort.Slice(profiles, func(i, j int) bool { return profiles[i].ID < profiles[j].ID })
	return profiles
}

func httpPublicationProfile() Profile {
	return Profile{
		ID: HTTPPublicationProfileID, Version: HTTPPublicationVersion,
		Description: "Molejo exact and subdomain-pool HTTP publication with real TLS and withdrawal",
		Scenarios: []Scenario{{
			ID: "exact-and-pool-publication", Description: "publish one application at exact and pooled HTTPS addresses",
			Required: true, Timeout: 10 * time.Minute, Run: runHTTPPublicationJourney,
		}},
	}
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
