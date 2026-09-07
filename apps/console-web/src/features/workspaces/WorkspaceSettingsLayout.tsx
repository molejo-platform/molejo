import type { ReactNode } from "react";

import { PageHeader, TabNav } from "../../shared/ui/Page";

export function WorkspaceSettingsLayout({ workspaceId, children }: { workspaceId: string; children: ReactNode }) {
  const params = { workspaceId };
  return (
    <div className="stack">
      <PageHeader
        eyebrow="Workspace"
        title="Configurações"
        description="Gerencie identidade, acesso e integrações deste Workspace."
      />
      <TabNav
        label="Configurações do Workspace"
        items={[
          { label: "Geral", to: "/workspaces/$workspaceId/settings", params },
          { label: "Membros", to: "/workspaces/$workspaceId/settings/members", params },
          { label: "Grupos", to: "/workspaces/$workspaceId/settings/groups", params },
          { label: "Acessos", to: "/workspaces/$workspaceId/settings/access", params },
          { label: "Auditoria", to: "/workspaces/$workspaceId/settings/audit", params },
          { label: "GitHub", to: "/workspaces/$workspaceId/settings/github", params },
        ]}
      />
      {children}
    </div>
  );
}
