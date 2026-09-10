# Armazenamento Kubernetes

A Molejo não instala nem gerencia um provisionador de armazenamento. O operador
do cluster seleciona uma `StorageClass`, comprova seu funcionamento localmente e
depois registra o binding na administração da instalação.

```shell
molejoctl capability storage verify \
  --kube-context meu-cluster \
  --storage-class standard

molejoctl capability storage smoke \
  --kube-context meu-cluster \
  --storage-class standard
```

`verify` apenas lê a `StorageClass`. `smoke` cria um namespace temporário,
solicita um volume pequeno `ReadWriteOnce`, monta, escreve e lê um arquivo de
prova e remove o namespace. Passar no smoke não registra um binding; o registro
continua sendo uma ação explícita e auditada do control plane.
