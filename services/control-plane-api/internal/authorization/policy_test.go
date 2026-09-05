package authorization

import "testing"

func TestWorkspacePolicy(t *testing.T) {
	tests := []struct {
		name       string
		context    Context
		permission Permission
		allowed    bool
	}{
		{name: "installation administrator creates workspace", context: Context{InstallationAdministrator: true}, permission: CreateWorkspace, allowed: true},
		{name: "installation administrator manages agents", context: Context{InstallationAdministrator: true}, permission: ManageAgents, allowed: true},
		{name: "workspace owner cannot manage agents", context: Context{MembershipRole: RoleOwner}, permission: ManageAgents, allowed: false},
		{name: "owner manages members", context: Context{MembershipRole: RoleOwner}, permission: ManageMembers, allowed: true},
		{name: "owner manages automation", context: Context{MembershipRole: RoleOwner}, permission: ManageAutomation, allowed: true},
		{name: "member cannot manage automation", context: Context{MembershipRole: RoleMember}, permission: ManageAutomation, allowed: false},
		{name: "member deploys", context: Context{MembershipRole: RoleMember}, permission: Deploy, allowed: true},
		{name: "viewer cannot deploy", context: Context{MembershipRole: RoleViewer}, permission: Deploy},
		{name: "suspended owner denied", context: Context{MembershipRole: RoleOwner, MembershipSuspended: true}, permission: ManageMembers},
		{name: "group grant adds permission", context: Context{MembershipRole: RoleViewer, Relations: []Relation{RelationDeployer}}, permission: Deploy, allowed: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Allowed(test.context, test.permission); got != test.allowed {
				t.Fatalf("Allowed() = %t, want %t", got, test.allowed)
			}
		})
	}
}
