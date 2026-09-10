import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { type FormEvent, useEffect, useMemo, useState } from "react";

import { userFacingError } from "../../shared/api/errors";
import type { ClusterStorageBinding } from "../../shared/api/types";
import { formatDateTime } from "../../shared/format";
import { Alert } from "../../shared/ui/Alert";
import { Button } from "../../shared/ui/Button";
import { DataList, DataListItem } from "../../shared/ui/DataList";
import { Field, SelectField } from "../../shared/ui/Field";
import { EmptyState, PageHeader } from "../../shared/ui/Page";
import { StatusBadge } from "../../shared/ui/StatusBadge";
import { useSessionQuery } from "../authentication/public";
import { activeClusters, clusterPlacementQueries } from "../cluster-placement/public";
import { deletePublicationBinding, deleteStorageBinding, putPublicationBinding, putStorageBinding } from "./api";
import { installationBindingKeys, installationBindingQueries } from "./queries";

export function ClusterBindingsPage() {
  const session = useSessionQuery();
  const clusters = useQuery(clusterPlacementQueries.installation());
  const availableClusters = useMemo(() => activeClusters(clusters.data?.items), [clusters.data?.items]);
  const [clusterId, setClusterId] = useState("");
  useEffect(() => {
    if (!availableClusters.some((cluster) => cluster.id === clusterId)) setClusterId(availableClusters[0]?.id ?? "");
  }, [availableClusters, clusterId]);

  if (session.data?.installationCapabilities.manageBindings !== true) {
    return (
      <div className="stack">
        <Alert>A administração da instalação é necessária.</Alert>
        <Link to="/">Voltar</Link>
      </div>
    );
  }

  return (
    <div className="stack">
      <PageHeader
        eyebrow="Instalação"
        title="Bindings do cluster"
        description="Seleções explícitas de infraestrutura. O Agent apenas verifica os recursos escolhidos."
        breadcrumbs={[{ label: "Visão geral", to: "/" }, { label: "Administração" }]}
      />
      {clusters.isError && <Alert>{userFacingError(clusters.error)}</Alert>}
      {clusters.isPending ? (
        <p role="status">Carregando clusters…</p>
      ) : availableClusters.length ? (
        <SelectField label="Cluster" value={clusterId} onChange={(event) => setClusterId(event.target.value)}>
          {availableClusters.map((cluster) => (
            <option key={cluster.id} value={cluster.id}>
              {cluster.name} · {cluster.kubernetesVersion ?? "Kubernetes"}
            </option>
          ))}
        </SelectField>
      ) : (
        <EmptyState title="Nenhum cluster ativo" description="Pareie um Cluster Agent antes de registrar bindings." />
      )}
      {clusterId && <ClusterBindingEditor clusterId={clusterId} />}
    </div>
  );
}

function ClusterBindingEditor({ clusterId }: { clusterId: string }) {
  const queryClient = useQueryClient();
  const storage = useQuery(installationBindingQueries.storage(clusterId));
  const publication = useQuery(installationBindingQueries.publication(clusterId));
  const metrics = useQuery(installationBindingQueries.metrics(clusterId));
  const [storageForm, setStorageForm] = useState({ profileId: "persistent-standard", storageClassName: "local-path" });

  async function refresh() {
    await queryClient.invalidateQueries({ queryKey: installationBindingKeys.all(clusterId) });
  }

  const saveStorage = useMutation({
    mutationFn: () => {
      const existing = storage.data?.items.find((item) => item.storageProfileId === storageForm.profileId);
      return putStorageBinding(clusterId, storageForm.profileId, storageForm.storageClassName, existing?.version);
    },
    onSuccess: refresh,
  });
  const removeStorage = useMutation({
    mutationFn: (binding: ClusterStorageBinding) => deleteStorageBinding(clusterId, binding),
    onSuccess: refresh,
  });
  const savePublication = useMutation({
    mutationFn: () => putPublicationBinding(clusterId, publication.data?.version),
    onSuccess: refresh,
  });
  const removePublication = useMutation({
    mutationFn: () => {
      if (!publication.data) throw new Error("Publication binding is not loaded");
      return deletePublicationBinding(clusterId, publication.data);
    },
    onSuccess: refresh,
  });
  const error =
    storage.error ??
    publication.error ??
    metrics.error ??
    saveStorage.error ??
    removeStorage.error ??
    savePublication.error ??
    removePublication.error;

  function submitStorage(event: FormEvent) {
    event.preventDefault();
    saveStorage.mutate();
  }

  return (
    <>
      <section className="panel stack">
        <div>
          <h2>Armazenamento</h2>
          <p className="muted">
            Execute primeiro `molejoctl capability storage smoke`. O registro abaixo nunca instala uma StorageClass.
          </p>
        </div>
        <form className="inline-form" onSubmit={submitStorage}>
          <Field
            label="Storage profile"
            value={storageForm.profileId}
            onChange={(event) => setStorageForm({ ...storageForm, profileId: event.target.value })}
            required
          />
          <Field
            label="StorageClass"
            value={storageForm.storageClassName}
            onChange={(event) => setStorageForm({ ...storageForm, storageClassName: event.target.value })}
            required
          />
          <Button type="submit" loading={saveStorage.isPending}>
            Registrar
          </Button>
        </form>
        {storage.isPending ? (
          <p role="status">Carregando bindings de armazenamento…</p>
        ) : storage.data?.items.length ? (
          <DataList>
            {storage.data.items.map((binding) => (
              <DataListItem key={binding.storageProfileId}>
                <span>
                  <strong>
                    {binding.storageProfileId} → {binding.storageClassName}
                  </strong>
                  <small>
                    {binding.provisioner || "Aguardando observação"} · versão {binding.version}
                    {binding.observedAt ? ` · ${formatDateTime(binding.observedAt)}` : ""}
                  </small>
                </span>
                <div className="row-controls">
                  <StatusBadge status={binding.health} label={binding.reasonCode || binding.health} />
                  <Button
                    variant="secondary"
                    loading={
                      removeStorage.isPending && removeStorage.variables?.storageProfileId === binding.storageProfileId
                    }
                    onClick={() => removeStorage.mutate(binding)}
                  >
                    Remover
                  </Button>
                </div>
              </DataListItem>
            ))}
          </DataList>
        ) : (
          <EmptyState
            title="Armazenamento não configurado"
            description="Aplicações stateless continuam independentes deste binding."
          />
        )}
      </section>
      <section className="panel stack">
        <div>
          <h2>Publicação HTTP</h2>
          <p className="muted">Convenção desta versão: `molejo-system/molejo#https-molejo`.</p>
        </div>
        {publication.isPending ? (
          <p role="status">Carregando publicação…</p>
        ) : publication.data ? (
          <DataList>
            <DataListItem>
              <span>
                <strong>
                  {publication.data.gatewayNamespace}/{publication.data.gatewayName}#{publication.data.sectionName}
                </strong>
                <small>
                  {publication.data.gatewayClassName || "Aguardando observação"} · versão {publication.data.version}
                </small>
              </span>
              <div className="row-controls">
                <StatusBadge
                  status={publication.data.health}
                  label={publication.data.reasonCode || publication.data.health}
                />
                <Button
                  variant="secondary"
                  loading={removePublication.isPending}
                  onClick={() => removePublication.mutate()}
                >
                  Remover
                </Button>
              </div>
            </DataListItem>
          </DataList>
        ) : (
          <EmptyState
            title="Publicação não configurada"
            description="Aplicações privadas continuam funcionando normalmente."
          />
        )}
        <Button variant="secondary" loading={savePublication.isPending} onClick={() => savePublication.mutate()}>
          {publication.data ? "Reverificar referência" : "Registrar referência convencional"}
        </Button>
      </section>
      <section className="panel stack">
        <h2>Métricas históricas</h2>
        {metrics.isPending ? (
          <p role="status">Carregando métricas históricas…</p>
        ) : metrics.data ? (
          <p>
            <StatusBadge status={metrics.data.health} label={metrics.data.reasonCode || metrics.data.health} /> Provider{" "}
            {metrics.data.provider}, versão {metrics.data.version}.
          </p>
        ) : (
          <EmptyState
            title="Não configurado"
            description="Métricas atuais do Kubernetes continuam disponíveis quando o Metrics API está saudável."
          />
        )}
      </section>
      {error && <Alert>{userFacingError(error)}</Alert>}
    </>
  );
}
