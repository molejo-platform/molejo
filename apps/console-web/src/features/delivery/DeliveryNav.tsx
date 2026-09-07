import { TabNav } from "../../shared/ui/Page";

export type DeliveryRouteParams = Record<"workspaceId" | "projectId" | "environmentId" | "appEnvironmentId", string>;

export function DeliveryNav({ params }: { params: DeliveryRouteParams }) {
  return (
    <TabNav
      label="Ciclo de entrega"
      items={[
        {
          label: "Implantações",
          to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/deployments",
          params,
        },
        {
          label: "Builds",
          to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/builds",
          params,
          exact: false,
        },
        {
          label: "Releases",
          to: "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/releases",
          params,
        },
      ]}
    />
  );
}
