# ADR 0021: bindings explícitos gestionados por el operador

## Estado

Aceptado para la arquitectura alfa.

## Contexto

Molejo debe componer con la infraestructura que el Cluster Operator ya confía,
sin seleccionarla, instalarla o reconfigurarla silenciosamente. Las observaciones
de capabilities pueden comprobar que existe una StorageClass, un Gateway o un
endpoint de telemetría, pero un candidato observado no representa consentimiento
para usarlo ni configuración durable del producto.

## Decisión

El Control Plane posee bindings de capabilities durables y tipados. Un Cluster
Operator crea, modifica o elimina un binding explícitamente mediante un flujo de
operación autenticado. `molejoctl` puede conducir ese flujo y ejecutar validación
local, pero no activa un candidato observado sin intención del operador.

El Cluster Agent reporta evidencia autenticada y reciente sobre el clúster y los
recursos referenciados. Nunca elige ni crea un binding del Control Plane.
Discovery puede presentar candidatos, pero la activación permanece explícita.

Cada capability recibe un contrato estrecho cuando se implementa su primera
fatia concreta, como métricas históricas, storage o publicación. Molejo no crea
un registry universal de providers, configuración JSON arbitraria ni lifecycle
genérico de plugins.

Feature Availability combina el binding durable con evidencia actual del Agent.
Un binding configurado sin evidencia reciente es `Unknown`; saludable y conforme
es `Available`; inaccesible es `Unavailable`; y sin binding es `NotConfigured`.

## Consecuencias

- Observar infraestructura no modifica configuración de producto silenciosamente.
- El Cluster Operator conserva el control de la stack que Molejo consume.
- Console y desarrolladores no reciben credenciales ni detalles de administración.
- `molejoctl` sigue siendo un runbook explícito, no un control plane implícito.
- Cada provider requiere un contrato concreto y evidencia de conformidad.

## Alternativas consideradas

La selección automática fue rechazada porque discovery no es consentimiento.
Bindings solamente en el clúster fueron rechazados porque impiden decisiones
estables multi-cluster. Una tabla universal de providers fue rechazada porque
oculta semántica, seguridad y salud específicas de cada capability.

## Referencias

- [ADR 0015: ownership de capacidades](0015-ownership-de-capacidades.md)
- [ADR 0018: observación de capacidades y disponibilidad](0018-observacion-de-capacidades-y-disponibilidad-de-features.md)
