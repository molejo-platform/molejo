import type { components } from "./generated/control-plane";

export type ActorRole = components["schemas"]["Session"]["actor"]["role"];
export type Session = components["schemas"]["Session"];
export type Workspace = components["schemas"]["Workspace"];
export type DeploymentIntent = components["schemas"]["DeploymentIntent"];
export type Deployment = components["schemas"]["Deployment"];
export type DeploymentState = Deployment["state"];
export type Operation = components["schemas"]["Operation"];
export type OperationKind = Operation["kind"];
export type OperationStatus = Operation["status"];
export type ApiError = components["schemas"]["Error"];
export type MutationAccepted = { deployment?: Deployment; operation: Operation };
