package domain

// ActorReference identifies the human or automation principal responsible for
// a historical action. DisplayName is a mutable label; ID is the stable
// identity used for attribution.
type ActorReference struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	DisplayName string `json:"displayName"`
}
