# Application loop

El application loop comienza cuando foundation y plataforma están listas. Un
desarrollador o CI registra una release de imagen inmutable, cambia el estado
deseado mediante la API de Molejo y observa la operación y el estado resultante de
la aplicación. El control plane autoriza y registra la intención; el Cluster Agent
outbound la entrega; el Platform Operator reconcilia los CRs de Molejo en
Kubernetes.

Sistemas de build, registries y engines GitOps son productores combinables. No
modifican directamente los recursos Kubernetes controlados por Molejo. La primera
integración documentada es la [CI externa](external-ci.md).
