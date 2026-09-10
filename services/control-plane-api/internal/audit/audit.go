package audit

import "encoding/json"

const (
	Succeeded = "Succeeded"
	Failed    = "Failed"
	Denied    = "Denied"
)

type Event struct {
	PublicID         string
	ActorUserID      *int64
	ActorPrincipalID *int64
	SessionID        *int64
	WorkspaceID      *int64
	Action           string
	TargetType       string
	TargetPublicID   string
	Outcome          string
	Reason           string
	RequestID        string
	TraceID          string
	SourceHash       []byte
	UserAgentHash    []byte
	Metadata         map[string]any
}

func (e Event) MetadataJSON() []byte {
	if e.Metadata == nil {
		return []byte("{}")
	}
	value, err := json.Marshal(e.Metadata)
	if err != nil {
		return []byte("{}")
	}
	return value
}
