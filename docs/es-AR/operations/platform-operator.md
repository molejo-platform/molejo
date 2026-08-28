# Operación del Platform Operator

Este runbook describe las señales expuestas por el primer controller de
`AppDeployment`. Las Conditions son la fuente duradera de verdad; Events, logs,
métricas y traces explican cómo el controller llegó al estado actual.

## Contrato de estado

| Condition activa | Significado | Primeras verificaciones |
| --- | --- | --- |
| `Ready=True` | Service y Deployment convergieron; un workload público también tiene un HTTPRoute actual y aceptado y un Gateway HTTPS compartido programado. | Confirmar Service, release observada, réplicas, parent del HTTPRoute, Gateway y listener `https-molejo`. |
| `Progressing=True` | El rollout del workload, la ruta o el Gateway compartido todavía está convergiendo. | Inspeccionar Deployment y, para workloads públicos, las Conditions del parent del HTTPRoute y del Gateway. |
| `Degraded=True` | Una falla conocida de workload, ownership, hostname, ruta o Gateway bloquea la convergencia. | Inspeccionar `reason`, Conditions de los hijos y del Gateway y Events. |

`DeploymentAvailable` identifica el estado listo. Los reasons de progreso son
`DeploymentProgressing`, `HTTPRouteProgressing` y `GatewayProgressing`. Los reasons
estables de degradación son `ProgressDeadlineExceeded`, `ReplicaFailure`,
`OwnershipConflict`, `HostnameConflict`, `HTTPRouteRejected`, `GatewayRejected` y
`ReconcileFailed`. Los conflictos de ownership y hostname y los errores
persistentes de la API se verifican cada cinco minutos sin usar el backoff de
error del controller. Las fallas transitorias de la API de Kubernetes usan el
backoff de error de controller-runtime.

## Validación de schema

Los campos desconocidos se rechazan cuando el cliente solicita
`FieldValidation=Strict` en el servidor. En los modos `Warn` o `Ignore`, el API
server de Kubernetes puede aceptar la solicitud y eliminar los campos
desconocidos. La instalación base no usa un admission webhook para cambiar este
comportamiento de Kubernetes.

## Contrato de runtime privado

Cada `AppDeployment` posee exactamente un Deployment y un Service ClusterIP con
el mismo nombre y namespace. El Service apunta al puerto `http` del container y
permanece privado aun cuando un HTTPRoute publica el workload. El spec exige un
digest inmutable de imagen, puerto,
requests y limits en millicores de CPU y MiB, además de los paths de liveness y
readiness. Los requests no pueden superar los limits.

El workload se ejecuta como non-root, con seccomp `RuntimeDefault`, sin privilege
escalation ni capabilities y con root filesystem de solo lectura. Startup usa el
path de readiness con una ventana de 60 segundos; readiness se ejecuta cada cinco
segundos y liveness cada diez. El operator no lee Pods ni EndpointSlices; el
status del Deployment sigue siendo la fuente del rollout.

## Contrato de publicación

`spec.exposure` tiene como default `Private`. Un workload privado no posee un
HTTPRoute y continúa accesible mediante su Service ClusterIP. Un workload público
requiere un label DNS en `spec.slug` y posee un HTTPRoute con el mismo nombre para
`{slug}.molejo.dev`. La ruta se conecta al listener `https-molejo` del Gateway
compartido `fruto`, en `fruto-system`, y reenvía al Service con el mismo nombre.

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

Cambiar un workload a `Private` elimina su HTTPRoute controlado sin eliminar el
Deployment ni el Service. Los objetos en eliminación no se reconcilian, y el
garbage collector de Kubernetes administra sus hijos controlados.

## Flujo de diagnóstico

Reemplazar el namespace y el nombre de ejemplo antes de ejecutar:

```bash
kubectl get appdeployment ap-example -n ws-example -o yaml
kubectl describe appdeployment ap-example -n ws-example
kubectl get deployment ap-example -n ws-example -o yaml
kubectl describe deployment ap-example -n ws-example
kubectl get service ap-example -n ws-example -o yaml
kubectl get httproute ap-example -n ws-example -o yaml
kubectl describe httproute ap-example -n ws-example
kubectl get gateway fruto -n fruto-system -o yaml
kubectl get pods -n ws-example -o wide
kubectl get events -n ws-example --sort-by=.metadata.creationTimestamp
kubectl logs deployment/platform-operator -n fruto-system --all-containers --prefix
```

Correlacionar logs y traces mediante `trace_id` y refinar la investigación con el
UID del recurso, la generación, el estado y el reason. Los mensajes del status se
sanitizan intencionalmente; los errores técnicos permanecen en logs y traces.

Los Events `DeploymentCreated`, `DeploymentUpdated`, `ServiceCreated`,
`ServiceUpdated`, `HTTPRouteCreated`, `HTTPRouteUpdated` y `HTTPRouteDeleted`
identifican transiciones de los hijos. La aplicación del Service y de la ruta se
rastrea mediante los spans `kubernetes.service.apply` y
`kubernetes.httproute.apply`. Las reconciliaciones repetidas ya convergidas no
emiten Events de transición duplicados.

## Verificaciones reproducibles

`just e2e` construye dos imágenes de la fixture localmente, carga ambas en un
cluster Kind descartable y valida HTTP privado, egress interno controlado, rollout
por digest, falla y recuperación de probes, drift, recreación de los hijos y
garbage collection. También instala un Gateway local y valida REST, GraphQL, SSE
incremental, WebSocket persistente, eliminación de la ruta y TLS con un
certificado efímero confiado por el cliente de prueba. `just e2e-public` agrega
una llamada HTTPS real de salida y queda deliberadamente fuera del gate
determinístico `just ci`.

Esta prueba local no valida DNS público de entrada ni un certificado con confianza
pública. Esos puntos permanecen como una etapa de aceptación separada en el
ambiente de foundation.

## Contratos de imagen para frontend

Las fixtures `static-html` y `vite-react-spa` son imágenes de referencia mantenidas
y usan el mismo runtime de AppDeployment. El operator también acepta imágenes HTTP
inmutables propias y no inspecciona su framework o servidor. Las referencias
escuchan en `8080`, exponen `/healthz` y `/readyz` y ejecutan NGINX como
`65532:65532` con root filesystem de solo lectura. El HTML estático devuelve `404`
para paths desconocidos. La SPA devuelve `index.html` con HTTP `200` para rutas del
navegador; su router del cliente es responsable por la página Not Found. Los
assets ausentes devuelven `404` y nunca reciben el shell de la SPA. HTML usa
`no-cache` y se revalida; los assets con fingerprint son inmutables por un año.

Ejecutar `just frontend-test` para probar el container restringido y `just e2e`
para el ciclo completo en Kind. Ejecutar `just audit-frontend-images` por separado
cuando se requiera una verificación con la base de vulnerabilidades de Docker
Scout; la auditoría mutable no integra el gate determinístico `just ci`.

`just e2e-frontend-k3s` es una aceptación separada solo para mantenedores. Requiere
Docker con push autenticado mediante Buildx, `curl`, `jq`, `kubectl`, `sed`, un
cluster exclusivamente amd64, el Gateway `fruto-system/fruto` programado y el
Secret de origen del registry `fruto-system/registry-pull`. El target usa
`--context fruto-lab` por default y rechaza otro contexto, salvo cuando
`FRUTO_KUBE_CONTEXT` y `FRUTO_ALLOW_CUSTOM_CONTEXT=true` sustituyen explícitamente
esa protección.

El target publica tres imágenes amd64 con tags temporales, crea o actualiza
`ws-e2e-static` y `ws-e2e-spa`, copia el Secret del registry en esos
namespaces, modifica sus ServiceAccounts `default` y mantiene ambos AppDeployments
y sus rutas públicas disponibles. Los archivos locales temporales se eliminan,
pero las imágenes del registry y los recursos estables del cluster se conservan
intencionalmente. Nunca debe agregarse a `just ci`.

## Salud y métricas

Liveness informa la salud del proceso en `/healthz`. Readiness responde con éxito
en `/readyz` solamente después de sincronizar el cache del manager. La instalación
base no expone el puerto de salud mediante un Service.

Las métricas están disponibles mediante HTTPS autenticado en
`platform-operator-metrics.fruto-system.svc:8443`. Los consumidores necesitan un
binding al ClusterRole `platform-operator-metrics-reader`. Además de las métricas
nativas de controller-runtime, el operator expone:

- `fruto_platform_operator_build_info`;
- `fruto_platform_operator_state_transitions_total`.

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
