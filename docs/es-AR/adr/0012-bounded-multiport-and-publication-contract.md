# ADR-0012: Bounded Multiport and Publication Contract

Status: Draft

## Context

Las aplicaciones pueden escuchar en más de un puerto y algunos protocolos no
pueden usar un proxy reverso HTTP. Exponer Services, NodePorts, listeners del
Gateway o load balancers del proveedor como configuración de producto acoplaría
a los usuarios a una instalación y llevaría las disputas de asignación a la API
pública.

## Decision

Cada configuración de AppEnvironment declara entre uno y ocho puertos TCP del
contenedor con nombre. Los probes de startup, readiness y liveness referencian un
puerto por nombre y pueden usar HTTP o TCP. La publicación es una lista separada
y limitada, con como máximo un endpoint HTTP y un endpoint TCP experimental.

El control plane, nunca el cliente, asigna los puertos TCP externos. PostgreSQL
mantiene claims transaccionales de hostnames y puertos externos y registra las
versiones de configuración deseada y actual. Desplegar una configuración
histórica vuelve a reservar sus claims antes de crear el snapshot inmutable del
Deployment. La API nunca acepta un puerto externo elegido por el cliente.

El Operator proyecta la intención en un único Service ClusterIP. La publicación
HTTP usa HTTPRoute y la TCP usa TCPRoute contra listeners con nombre en el
Gateway compartido. El estado se informa por endpoint. La instalación anuncia
una capacidad TCP exacta y proporciona de forma privada el pool limitado de
listeners, NodePorts, proxy y firewall.

## Consequences

El contrato del producto permanece portable entre proveedores Kubernetes,
mientras que el laboratorio puede exponer PostgreSQL, Redis, RabbitMQ y servicios
TCP similares. La capacidad es explícita y fail-closed: el agotamiento o un
conflicto devuelve un error estable en lugar de tomar una dirección ajena.

El primer incremento solo admite TCP, asignación automática, una publicación
HTTP y una TCP por AppEnvironment y un pool de laboratorio de 16 puertos. No
admite UDP, enrutamiento TLS o SNI, puertos externos arbitrarios, múltiples
publicaciones TCP, dominios personalizados, claims compartidos ni garantías de
disponibilidad de producción.

## References

- [ADR-0004: Publication and HTTP Transport Contract](0004-publication-and-http-transport-contract.md)
- [ADR-0006: Control Plane Topology and Runtime Boundary](0006-control-plane-topology-and-runtime-boundary.md)
- [Gateway API TCPRoute](https://gateway-api.sigs.k8s.io/api-types/tcproute/)
