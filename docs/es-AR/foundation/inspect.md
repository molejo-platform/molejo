# Inspección de la foundation

`molejoctl foundation inspect --kube-context <contexto>` lee el clúster seleccionado
e informa distribución y versión de Kubernetes, nodes, StorageClass por defecto,
disponibilidad de Gateway API y componentes Molejo instalados. No cambia el clúster
ni declara soporte formal para una distribución Kubernetes.

Usá el informe antes de instalar la plataforma y para diagnosticar drift del
entorno. Creación del clúster, ciclo de vida de nodes, CNI, IAM, DNS, firewall y
load balancer específicos del proveedor quedan fuera de Molejo.
