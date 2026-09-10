import type { FormEvent, ReactNode } from "react";

import { userFacingError } from "../../shared/api/errors";
import type { AppEnvironment, Parameter, RuntimeConfiguration } from "../../shared/api/types";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { Field } from "../../shared/ui/Field";
import { TabNav } from "../../shared/ui/Page";
import { EnvironmentAppLayout, type EnvironmentParams } from "../app-environments/public";
import { type RuntimeConfigurationSection, useRuntimeConfigurationEditor } from "./useRuntimeConfigurationEditor";

type Section = RuntimeConfigurationSection;

export function ConfigurationNav({
  params,
  workloadKind,
}: {
  params: EnvironmentParams;
  workloadKind: AppEnvironment["workloadKind"];
}) {
  const routeParams = {
    workspaceId: params.workspaceId,
    projectId: params.projectId,
    environmentId: params.environmentId,
    appEnvironmentId: params.appEnvironmentId,
  };
  const base =
    "/workspaces/$workspaceId/projects/$projectId/environments/$environmentId/apps/$appEnvironmentId/settings";
  const items = [
    { label: "Build e branch", to: `${base}/build`, params: routeParams },
    { label: "Variáveis", to: base, params: routeParams },
    { label: "Parameters", to: `${base}/secrets`, params: routeParams },
    { label: "Rede", to: `${base}/network`, params: routeParams },
    { label: "Health checks", to: `${base}/health`, params: routeParams },
    { label: "Recursos", to: `${base}/resources`, params: routeParams },
    { label: "Versões", to: `${base}/versions`, params: routeParams },
  ];
  if (workloadKind === "Stateful")
    items.splice(6, 0, { label: "Armazenamento", to: `${base}/storage`, params: routeParams });
  return <TabNav label="Configuração do runtime" items={items} />;
}

export function createRuntimeConfigurationPage(
  section: Section,
  title: string,
  description: string,
  render: (
    draft: RuntimeConfiguration,
    setDraft: (value: RuntimeConfiguration) => void,
    parameters: Parameter[],
    disabled: boolean,
    onValidityChange: (valid: boolean) => void,
    target: AppEnvironment,
  ) => ReactNode,
) {
  return function ConfigurationPage() {
    return (
      <EnvironmentAppLayout>
        {(target, params) => (
          <section className="stack">
            <ConfigurationNav params={params} workloadKind={target.workloadKind} />
            <ConfigurationEditor
              target={target}
              params={params}
              section={section}
              title={title}
              description={description}
              render={render}
            />
          </section>
        )}
      </EnvironmentAppLayout>
    );
  };
}
export function ConfigurationEditor({
  target,
  params,
  section,
  title,
  description,
  render,
}: {
  target: AppEnvironment;
  params: EnvironmentParams;
  section: Section;
  title: string;
  description: string;
  render: (
    draft: RuntimeConfiguration,
    setDraft: (value: RuntimeConfiguration) => void,
    parameters: Parameter[],
    disabled: boolean,
    onValidityChange: (valid: boolean) => void,
    target: AppEnvironment,
  ) => ReactNode;
}) {
  const viewModel = useRuntimeConfigurationEditor(target, params, section);
  const { branch, canMutate, capabilities, conflict, dirty, draft, parameters, save, valid } = viewModel;
  function submit(event: FormEvent) {
    event.preventDefault();
    if (valid) viewModel.saveDesired();
  }
  return (
    <section className="stack">
      <div>
        <p className="eyebrow">Configuração</p>
        <h2>{title}</h2>
        <p className="muted">{description}</p>
      </div>
      {save.isSuccess && (
        <Alert tone="success">
          Configuração salva como estado desejado. Implante a nova versão quando estiver pronta.
        </Alert>
      )}
      {conflict && (
        <Alert tone="warning">
          Outra pessoa alterou este runtime. Suas edições foram preservadas. Revise a versão atual e escolha se deseja
          reaplicá-las.
        </Alert>
      )}
      {save.isError && !conflict && <Alert>{userFacingError(save.error)}</Alert>}
      {capabilities.error && <Alert>{userFacingError(capabilities.error)}</Alert>}
      {parameters.error && <Alert>{userFacingError(parameters.error)}</Alert>}
      <form className="panel stack" onSubmit={submit}>
        {section === "build" ? (
          <Field
            label="Branch principal deste Environment"
            helper="Novos builds resolvem um SHA desta branch."
            value={branch}
            onChange={(event) => viewModel.updateBranch(event.target.value)}
            maxLength={255}
            disabled={!canMutate}
            required
          />
        ) : (
          render(draft, viewModel.updateDraft, parameters.data?.items ?? [], !canMutate, viewModel.setValid, target)
        )}
        {canMutate && (
          <div className="form-actions">
            {conflict && (
              <Button
                type="button"
                variant="secondary"
                onClick={() => {
                  viewModel.reset();
                }}
              >
                Usar versão atual
              </Button>
            )}
            {conflict && (
              <Button type="button" variant="secondary" onClick={viewModel.saveDesired}>
                Reaplicar minhas alterações
              </Button>
            )}
            <Button
              type="submit"
              loading={save.isPending}
              disabled={!dirty || !valid || (section === "build" && !branch.trim())}
            >
              Salvar estado desejado
            </Button>
          </div>
        )}
      </form>
    </section>
  );
}
