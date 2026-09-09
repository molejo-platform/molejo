# ADR 0018: observación de capacidades y disponibilidad de features

## Estado

Aceptado para la arquitectura alfa.

## Decisión

Molejo usa cuatro contratos separados:

1. **Protocol Capability** negocia comportamiento opcional entre binarios y no
   prueba que el clúster o provider esté utilizable.
2. **Capability Observation** es un hecho limitado y fechado reportado por el
   Cluster Agent autenticado sin modificar estado deseado.
3. **Provider Binding** conecta de manera tipada una necesidad del producto con
   una implementación externa, sin ser un registry genérico ni exponer
   credenciales o endpoints.
4. **Feature Availability** es una proyección read-only derivada por el control
   plane a partir de estado de producto, observaciones recientes, protocolo y
   bindings.

Los identificadores son neutrales de provider. La disponibilidad está limitada a
Workspace, App o AppEnvironment y usa `Available`, `Limited`, `NotConfigured`,
`Unavailable`, `Unsupported` o `Unknown`. La autorización de actores es una
decisión separada.

El horario de recepción del control plane es autoritativo. Una observación
expirada o Agent desconectado produce `Unknown`. Un snapshot completo sustituye
al anterior atómicamente; uno parcial actualiza solamente los datos presentes.

Telemetría actual e histórica son capacidades diferentes y nunca se sustituyen
silenciosamente.

## Ownership

- Cluster Agent observa y reporta hechos del clúster.
- Control plane posee bindings, freshness, resolución y proyección pública.
- Platform Operator reconcilia recursos de runtime de Molejo.
- `molejoctl` inspecciona localmente, pero no sobrescribe observaciones.
- Console combina disponibilidad y autorización solamente para presentación.

## Consecuencias

La interfaz puede explicar flujos no disponibles antes de una falla, los hechos
de un clúster no satisfacen otro y los providers opcionales siguen componibles.
Las observaciones nunca mutan infraestructura ni estado deseado.

## Referencias

- [ADR 0015: ownership de capacidades](0015-ownership-de-capacidades.md)
- [ADR 0016: política de ciclo de vida alfa](0016-politica-de-ciclo-de-vida-alfa.md)
