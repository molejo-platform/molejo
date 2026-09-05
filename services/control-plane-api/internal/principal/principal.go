// Package principal defines the authenticated identity used by control-plane
// application services independently of its authentication mechanism.
package principal

const (
	KindUser           = "User"
	KindServiceAccount = "ServiceAccount"
	KindSystem         = "System"
)

type Principal struct {
	ID               int64
	PublicID         string
	Kind             string
	DisplayName      string
	WorkspaceID      int64
	ProjectID        int64
	AppID            int64
	ServiceAccountID int64
}
