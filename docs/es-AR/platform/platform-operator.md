# Operación del Platform Operator

Este runbook describe las señales expuestas por el controller de `AppDeployment`.
Las Conditions son la fuente duradera de verdad; Events, logs, métricas y traces
explican cómo el controller llegó al estado actual.

## Contrato de estado

| Condition activa | Significado | Primeras verificaciones |
| --- | --- | --- |
| `Ready=True` | Service y el Deployment o StatefulSet seleccionado convergieron; todos los endpoints públicos también convergieron mediante el Gateway compartido. | Confirmar Service, workload, release observada, réplicas, Routes, Gateway y listener correspondiente. |
| `Progressing=True` | El rollout del workload, la ruta o el Gateway compartido todavía está convergiendo. | Inspeccionar el workload seleccionado y, para workloads públicos, las Conditions del parent de la Route y del Gateway. |
| `Degraded=True` | Una falla conocida de workload, ownership, hostname, ruta o Gateway bloquea la convergencia. | Inspeccionar `reason`, Conditions de los hijos y del Gateway y Events. |

`DeploymentAvailable` y `StatefulSetAvailable` identifican estados listos del
workload. Los reasons de progreso incluyen `DeploymentProgressing`,
`StatefulSetProgressing`, `StatefulSetReplacingStalePod`, `HTTPRouteProgressing` y
`GatewayProgressing`. Los reasons estables de degradación son
`ProgressDeadlineExceeded`, `ReplicaFailure`, `OwnershipConflict`,
`HostnameConflict`, `HTTPRouteRejected`, `GatewayRejected`,
`PublicationRejected` y `ReconcileFailed`. Los conflictos de ownership y hostname
y los errores persistentes de la API se verifican cada cinco minutos sin usar el
backoff de error del controller. Las fallas transitorias de la API de Kubernetes
usan el backoff de error de `controller-runtime`.

## Validación de schema

Los campos desconocidos se rechazan cuando el cliente solicita
`FieldValidation=Strict` en el servidor. En los modos `Warn` o `Ignore`, el API
server de Kubernetes puede aceptar la solicitud y eliminar los campos
desconocidos. La instalación base no usa un admission webhook para cambiar este
comportamiento de Kubernetes.

## Contrato de runtime privado

Cada `AppDeployment` posee exactamente un workload y un Service ClusterIP con el
mismo nombre y namespace. El Service expone entre uno y ocho puertos TCP del
container con nombre y permanece privado cuando HTTPRoute o TCPRoute publica
puertos seleccionados. El spec exige un digest inmutable de imagen, requests y
limits en millicores de CPU y MiB y probes HTTP o TCP de startup, readiness y
liveness que referencian un puerto con nombre. Los requests no pueden superar los
limits.

El workload se ejecuta como non-root, con seccomp `RuntimeDefault`, sin privilege
escalation ni capabilities y con root filesystem de solo lectura. Startup tiene
una ventana de 60 segundos; readiness se ejecuta cada cinco segundos y liveness
cada diez. El operator no lee Pods ni EndpointSlices; el status del workload
sigue siendo la fuente del rollout.

## Contrato de publicación

`spec.publicEndpoints` contiene como máximo una publicación HTTP y una TCP
experimental. HTTP se conecta al listener `https-molejo` y sirve
el hostname exacto resuelto por el control plane a partir de `domainId` y
`hostnameLabel`. El catálogo inicial ofrece `molejo.dev` a ambos tipos de
workload y `stateful.molejo.dev` solo a workloads Stateful. TCP se conecta al listener preasignado
`tcp-{externalPort}`. Ambas rutas reenvían a un puerto con nombre del Service con
el mismo nombre. Una lista vacía mantiene privado al workload. El Agent emite el
contrato actual de puertos con nombre, y la API rechaza puertos ausentes o probes
incompletos.

El operator considera que la publicación convergió solamente cuando el parent
esperado de la ruta tiene Conditions `Accepted=True` y `ResolvedRefs=True` de la
generación actual, el Gateway compartido tiene `Programmed=True` actual y su único
listener `https-molejo` tiene `Accepted=True`, `Programmed=True` y `ResolvedRefs=True`
actuales. Múltiples controllers informando el mismo parent efectivo de la ruta son
ambiguos y mantienen la ruta en progreso. Un estado ausente u obsoleto del
Gateway/listener informa `GatewayProgressing` mientras el Gateway converge. Un
Gateway ausente, un Gateway/listener actualmente rechazado o la ausencia de un
único listener `https-molejo` después de que el Gateway informa `Programmed=True` informa
`GatewayRejected`. Una falla conocida del Deployment tiene precedencia sobre el
progreso de publicación. Claims duplicados de hostname convergen a un owner
determinístico; el perdedor elimina solo su propia ruta, informa
`HostnameConflict`, preserva los campos de la release observada y vuelve a
intentar después de cinco minutos.

Eliminar un endpoint elimina solamente su Route controlada sin eliminar el
workload ni el Service. Los objetos en eliminación no se reconcilian, y el
garbage collector de Kubernetes administra sus hijos controlados.

## Flujo de diagnóstico

Reemplazar el namespace y el nombre de ejemplo antes de ejecutar:

```bash
kubectl get appdeployment ap-example -n ws-example -o yaml
kubectl describe appdeployment ap-example -n ws-example
kubectl get deployment ap-example -n ws-example -o yaml
kubectl describe deployment ap-example -n ws-example
kubectl get statefulset ap-example -n ws-example -o yaml
kubectl describe statefulset ap-example -n ws-example
kubectl get service ap-example -n ws-example -o yaml
kubectl get httproute ap-example -n ws-example -o yaml
kubectl describe httproute ap-example -n ws-example
kubectl get tcproute ap-example -n ws-example -o yaml
kubectl get gateway molejo -n molejo-system -o yaml
kubectl get pods -n ws-example -o wide
kubectl get events -n ws-example --sort-by=.metadata.creationTimestamp
kubectl logs deployment/platform-operator -n molejo-system --all-containers --prefix
```

Correlacionar logs y traces mediante `trace_id` y refinar la investigación con el
UID del recurso, la generación, el estado y el reason. Los mensajes del status se
sanitizan intencionalmente; los errores técnicos permanecen en logs y traces.

Los Events del workload, Service y Route identifican transiciones de los hijos.
La aplicación del Service y del HTTPRoute se rastrea mediante los spans
`kubernetes.service.apply` y `kubernetes.httproute.apply`. Las reconciliaciones
repetidas ya convergidas no emiten Events de transición duplicados.

## Verificaciones reproducibles

`just molejo-conformance kind` crea instancias locales y descartables de Kind
y del registry, empaqueta los mismos charts consumidos por `molejoctl` y valida
la instalación idempotente del runtime y del Control Plane. La jornada crea un
Workspace con límites de RBAC, registra una imagen OCI inmutable, reconcilia y
observa una aplicación privada, cancela su stream de logs, elimina la aplicación
de forma idempotente y comprueba el teardown del ambiente.

Esta prueba local no valida DNS público de entrada ni un certificado con confianza
pública. Esos puntos permanecen como una etapa de aceptación separada en el
ambiente de foundation.

## Salud y métricas

Liveness informa la salud del proceso en `/healthz`. Readiness responde con éxito
en `/readyz` solamente después de sincronizar el cache del manager. La instalación
base no expone el puerto de salud mediante un Service.

## Apagado ordenado

Al recibir `SIGTERM` o `SIGINT`, readiness falla inmediatamente y el manager
dispone de hasta 20 segundos para detener controllers, caches y servidores
internos. Después, tracing usa un contexto nuevo para un flush limitado a cinco
segundos. El Pod concede 30 segundos en total, preservando un margen para la salida
del proceso antes de que Kubernetes pueda forzar la terminación.

Una reconciliación interrumpida por el apagado del proceso es trabajo incompleto,
no degradación del workload. No registra `ReconcileFailed`; el estado deseado
permanece durable y es retomado por la siguiente instancia del manager. Una
segunda señal representa una terminación forzada explícita y no garantiza el
drenaje ni la exportación de traces.

Las métricas están disponibles mediante HTTPS autenticado en
`platform-operator-metrics.molejo-system.svc:8443`. Los consumidores necesitan un
binding al ClusterRole `platform-operator-metrics-reader`. Además de las métricas
nativas de controller-runtime, el operator expone:

- `molejo_platform_operator_build_info`;
- `molejo_platform_operator_state_transitions_total`.

Los nombres de recursos, namespaces, UIDs, digests de imágenes y trace IDs se
excluyen deliberadamente de las labels de métricas.

## Tracing opcional

Tracing está deshabilitado por defecto y no se instala ningún collector.
Configurar el Deployment con variables estándar de OpenTelemetry para habilitar
la exportación OTLP:

```yaml
env:
  - name: OTEL_TRACES_EXPORTER
    value: otlp
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: http://opentelemetry-collector.observability.svc:4318
  - name: OTEL_EXPORTER_OTLP_PROTOCOL
    value: http/protobuf
```

Una configuración inválida impide el startup. La indisponibilidad del collector
puede descartar telemetría, pero no interrumpe la reconciliación. Durante el
cierre, el operator intenta enviar los traces dentro de un tiempo limitado.
