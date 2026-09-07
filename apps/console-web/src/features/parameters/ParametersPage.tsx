import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { type FormEvent, useState } from "react";

import { userFacingError } from "../../shared/api/errors";
import type { Parameter, ParameterInput } from "../../shared/api/types";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { ConfirmAction } from "../../shared/ui/ConfirmAction";
import { Field, SelectField, TextareaField } from "../../shared/ui/Field";
import { EmptyState, PageHeader } from "../../shared/ui/Page";
import { useEffectiveCapabilities } from "../workspace-access/public";
import { archiveParameter, createParameter, listParameters, replaceParameter } from "./api";
import { parameterKeys } from "./queries";

const emptyInput: ParameterInput = { path: "", type: "PlainText", description: "", value: "" };

export function ParametersPage() {
  const { workspaceId } = useParams({ from: "/protected/workspaces/$workspaceId/parameters" });
  const capabilities = useEffectiveCapabilities(workspaceId, "Workspace", workspaceId);
  const canMutate = capabilities.data?.editResources === true;
  const queryClient = useQueryClient();
  const [showCreate, setShowCreate] = useState(false);
  const [editing, setEditing] = useState<Parameter>();
  const query = useQuery({
    queryKey: parameterKeys.list(workspaceId),
    queryFn: ({ signal }) => listParameters(workspaceId, signal),
  });
  const create = useMutation({
    mutationFn: (input: ParameterInput) => createParameter(workspaceId, input),
    onSuccess: async () => {
      setShowCreate(false);
      await queryClient.invalidateQueries({ queryKey: parameterKeys.list(workspaceId) });
    },
  });
  const replace = useMutation({
    mutationFn: ({ parameter, input }: { parameter: Parameter; input: ParameterInput }) =>
      replaceParameter(workspaceId, parameter, input),
    onSuccess: async () => {
      setEditing(undefined);
      await queryClient.invalidateQueries({ queryKey: parameterKeys.list(workspaceId) });
    },
  });
  const archive = useMutation({
    mutationFn: (parameter: Parameter) => archiveParameter(workspaceId, parameter),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: parameterKeys.list(workspaceId) }),
  });
  const error = capabilities.error ?? query.error ?? create.error ?? replace.error ?? archive.error;

  return (
    <div className="stack">
      <PageHeader
        eyebrow="Workspace"
        title="Parameters"
        description="Centralize valores reutilizáveis. Segredos são write-only e nunca voltam pela API."
        actions={
          canMutate ? (
            <Button
              onClick={() => {
                setEditing(undefined);
                setShowCreate((value) => !value);
              }}
            >
              {showCreate ? "Fechar" : "Novo Parameter"}
            </Button>
          ) : undefined
        }
      />
      {error && <Alert>{userFacingError(error)}</Alert>}
      {capabilities.isSuccess && !canMutate && (
        <Alert tone="info">Sua identidade não possui a capacidade de alterar Parameters.</Alert>
      )}
      {showCreate && canMutate && (
        <ParameterForm
          submitLabel="Criar Parameter"
          onSubmit={(input) => create.mutate(input)}
          pending={create.isPending}
        />
      )}
      <section className="panel stack">
        <div>
          <p className="eyebrow">Catálogo</p>
          <h2>Valores do Workspace</h2>
          <p className="muted">
            O path organiza o catálogo; uso por Apps será sempre configurado explicitamente em cada Environment.
          </p>
        </div>
        {query.isPending ? (
          <p role="status" className="muted">
            Carregando Parameters…
          </p>
        ) : query.isError ? null : query.data?.items.length ? (
          <div className="data-list">
            {query.data.items.map((parameter) => (
              <div className="stack data-row" key={parameter.id}>
                <span>
                  <strong className="mono">{parameter.path}</strong>
                  <small>
                    {parameter.type === "Secret" ? "Secret · valor protegido" : `PlainText · ${parameter.value ?? ""}`}{" "}
                    · versão {parameter.currentVersion}
                  </small>
                  {parameter.description && <small>{parameter.description}</small>}
                </span>
                {canMutate && (
                  <div className="form-actions">
                    <Button
                      variant="ghost"
                      onClick={() => {
                        setShowCreate(false);
                        setEditing(editing?.id === parameter.id ? undefined : parameter);
                      }}
                    >
                      {editing?.id === parameter.id ? "Fechar" : parameter.type === "Secret" ? "Substituir" : "Editar"}
                    </Button>
                    <ConfirmAction
                      trigger="Arquivar"
                      title={`Arquivar ${parameter.path}?`}
                      description="O Parameter deixará de aparecer no catálogo. Parameters vinculados serão protegidos contra remoção em uma próxima etapa."
                      confirmLabel="Arquivar Parameter"
                      onConfirm={() => archive.mutateAsync(parameter)}
                      pending={archive.isPending}
                    />
                  </div>
                )}
                {editing?.id === parameter.id && (
                  <ParameterForm
                    parameter={parameter}
                    submitLabel={parameter.type === "Secret" ? "Substituir segredo" : "Salvar nova versão"}
                    onSubmit={(input) => replace.mutate({ parameter, input })}
                    pending={replace.isPending}
                  />
                )}
              </div>
            ))}
          </div>
        ) : (
          <EmptyState
            title="Nenhum Parameter"
            description="Crie um texto comum ou um segredo protegido para começar."
            action={
              canMutate && !showCreate ? <Button onClick={() => setShowCreate(true)}>Novo Parameter</Button> : undefined
            }
          />
        )}
      </section>
    </div>
  );
}

function ParameterForm({
  parameter,
  submitLabel,
  onSubmit,
  pending,
}: {
  parameter?: Parameter;
  submitLabel: string;
  onSubmit: (input: ParameterInput) => void;
  pending: boolean;
}) {
  const [input, setInput] = useState<ParameterInput>(() =>
    parameter
      ? {
          path: parameter.path,
          type: parameter.type,
          description: parameter.description,
          value: parameter.type === "PlainText" ? (parameter.value ?? "") : "",
        }
      : emptyInput,
  );
  function submit(event: FormEvent) {
    event.preventDefault();
    onSubmit({ ...input, path: input.path.trim(), description: input.description?.trim() ?? "" });
  }
  return (
    <form className="panel stack" onSubmit={submit}>
      <div className="form-row">
        <Field
          label="Path"
          helper="Ex.: /shared/database/host"
          value={input.path}
          onChange={(event) => setInput({ ...input, path: event.target.value })}
          maxLength={255}
          pattern="^/[a-zA-Z0-9._/-]+$"
          required
        />
        <SelectField
          label="Tipo"
          value={input.type}
          onChange={(event) => setInput({ ...input, type: event.target.value as ParameterInput["type"], value: "" })}
          disabled={Boolean(parameter)}
          required
        >
          <option value="PlainText">PlainText</option>
          <option value="Secret">Secret</option>
        </SelectField>
      </div>
      <TextareaField
        label="Descrição"
        value={input.description ?? ""}
        onChange={(event) => setInput({ ...input, description: event.target.value })}
        maxLength={500}
      />
      <Field
        label={input.type === "Secret" ? (parameter ? "Novo valor secreto" : "Valor secreto") : "Valor"}
        type={input.type === "Secret" ? "password" : "text"}
        helper={input.type === "Secret" ? "Após salvar, o valor não poderá ser revelado ou copiado." : undefined}
        value={input.value}
        onChange={(event) => setInput({ ...input, value: event.target.value })}
        maxLength={65536}
        autoComplete="off"
        required
      />
      <div className="form-actions">
        <Button type="submit" loading={pending}>
          {submitLabel}
        </Button>
      </div>
    </form>
  );
}
