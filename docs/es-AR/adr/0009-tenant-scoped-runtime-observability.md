# ADR-0009: Tenant-Scoped Runtime Observability

Status: Draft

## Context

Los usuarios necesitan logs de runtime, métricas básicas y eventos operacionales
sin acceder a Kubernetes ni a los backends de almacenamiento. Las consultas
atraviesan los límites de Workspace, Project, App y AppEnvironment, mientras que
los backends de telemetría son componentes operacionales cuya disponibilidad y
topología pueden cambiar independientemente de los workloads administrados.

## Decision

La API pública resuelve la jerarquía completa del producto e inyecta el Namespace
y la identidad confiable del runtime en cada consulta. Los clientes no pueden
proporcionar selectores de tenant. Las consultas históricas están limitadas a 24
horas y a volúmenes de resultados definidos; los logs live usan SSE autenticado
con límites de concurrencia por actor y duración. Los snapshots actuales de
métricas usan un presupuesto SSE autenticado separado y un intervalo de consulta
alineado con la recolección. Ambos streams envían heartbeats, expiran y revalidan
la autorización mientras están conectados.

Cada log almacenado recibe un identificador y timestamp de ingesta estables. La
navegación histórica usa cursores opacos por clave sobre un snapshot fijo; el SSE
live envía lotes limitados cuyo ID de evento es un cursor opaco reanudable. La
reconexión mediante `Last-Event-ID` es, por lo tanto, idempotente y no depende de
comparar el contenido de los mensajes ni de una única ventana de polling.

OpenTelemetry Collectors forman la frontera portátil de ingestión. Un agente por
nodo recolecta logs de containers y métricas del kubelet, mientras que un
collector de cluster reúne eventos de Kubernetes. Un gateway enriquece y exporta
las señales. El despliegue de laboratorio utiliza ClickHouse para logs/eventos de
corta duración y VictoriaMetrics para métricas, protegidos por NetworkPolicies y
Services internos. Son adapters reemplazables de despliegue, no contratos
públicos del producto. La disponibilidad de las aplicaciones y la readiness del
control plane no dependen del almacenamiento de telemetría.

La Console mantiene el diagnóstico de build separado de la observabilidad de
runtime y ofrece un resumen y vistas dedicadas de logs, métricas y eventos. Los
estados vacíos, de carga, parciales y no disponibles son explícitos. Los eventos
de runtime exponen mensajes estables y sanitizados del producto, nunca nombres de
objetos, UIDs o mensajes crudos de Kubernetes. Traces, dashboards públicos,
alertas, retención prolongada y backup quedan fuera de esta decisión.

Cada vista de AppEnvironment incluye un marcador operacional compacto. Las
métricas permanecen live sólo mientras la vista está visible; la búsqueda
histórica de logs es el comportamiento predeterminado y el live tail es
explícito. La telemetría desconocida o atrasada sigue siendo distinguible de
cero, y los eventos de despliegue pueden correlacionarse con los gráficos
históricos.
La Console compone páginas históricas y lotes live en una store dedicada y
limitada, publica actualizaciones con una cadencia controlada y virtualiza las
filas renderizadas. Informa cuando los registros antiguos salen de la vista local
y pausa el seguimiento automático cuando el usuario se aleja de los registros
más recientes.

## Consequences

El aislamiento por Workspace se aplica centralmente y puede probarse sin
depender de los motores de almacenamiento. La topología de ingestión y
almacenamiento puede escalar o reemplazarse sin modificar la API o la Console. La
stack inicial del laboratorio es single-replica y pre-alpha; no declara HA,
recuperación ante desastres, retención prolongada ni aislamiento contra tenants
hostiles.
