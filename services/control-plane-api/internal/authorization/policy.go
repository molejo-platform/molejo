package authorization

type (
	Permission string
	Relation   string
)

const (
	CreateWorkspace  Permission = "installation.workspace.create"
	ManageUsers      Permission = "installation.users.manage"
	ManageAgents     Permission = "installation.agents.manage"
	ReadWorkspace    Permission = "workspace.read"
	ManageWorkspace  Permission = "workspace.manage"
	ManageMembers    Permission = "workspace.members.manage"
	ManageGroups     Permission = "workspace.groups.manage"
	ReadAudit        Permission = "workspace.audit.read"
	ManageAutomation Permission = "workspace.automation.manage"
	EditResources    Permission = "resource.edit"
	Deploy           Permission = "app_environment.deploy"
)

const (
	RoleOwner  = "Owner"
	RoleMember = "Member"
	RoleViewer = "Viewer"

	RelationViewer   Relation = "Viewer"
	RelationEditor   Relation = "Editor"
	RelationDeployer Relation = "Deployer"
	RelationManager  Relation = "Manager"
)

type Context struct {
	InstallationAdministrator bool
	MembershipRole            string
	MembershipSuspended       bool
	Relations                 []Relation
}

type Capabilities struct {
	ReadWorkspace    bool
	ManageWorkspace  bool
	ManageMembers    bool
	ManageGroups     bool
	ReadAudit        bool
	ManageAutomation bool
	EditResources    bool
	Deploy           bool
}

func EffectiveCapabilities(context Context) Capabilities {
	return Capabilities{
		ReadWorkspace:    Allowed(context, ReadWorkspace),
		ManageWorkspace:  Allowed(context, ManageWorkspace),
		ManageMembers:    Allowed(context, ManageMembers),
		ManageGroups:     Allowed(context, ManageGroups),
		ReadAudit:        Allowed(context, ReadAudit),
		ManageAutomation: Allowed(context, ManageAutomation),
		EditResources:    Allowed(context, EditResources),
		Deploy:           Allowed(context, Deploy),
	}
}

func Allowed(context Context, permission Permission) bool {
	if permission == CreateWorkspace || permission == ManageUsers || permission == ManageAgents {
		return context.InstallationAdministrator
	}
	if context.MembershipSuspended || context.MembershipRole == "" {
		return false
	}
	if permission == ReadWorkspace {
		return true
	}
	if context.MembershipRole == RoleOwner {
		return true
	}
	if context.MembershipRole == RoleMember && (permission == EditResources || permission == Deploy) {
		return true
	}
	for _, relation := range context.Relations {
		switch relation {
		case RelationManager:
			return true
		case RelationEditor:
			if permission == EditResources || permission == Deploy {
				return true
			}
		case RelationDeployer:
			if permission == Deploy {
				return true
			}
		case RelationViewer:
			if permission == ReadWorkspace {
				return true
			}
		}
	}
	return false
}
