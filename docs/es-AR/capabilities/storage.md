# Almacenamiento Kubernetes

Molejo no instala ni administra un provisionador de almacenamiento. El operador
del clúster elige una `StorageClass`, prueba su funcionamiento localmente y luego
registra el binding en la administración de la instalación.

```shell
molejoctl capability storage verify \
  --kube-context mi-cluster \
  --storage-class standard

molejoctl capability storage smoke \
  --kube-context mi-cluster \
  --storage-class standard
```

`verify` solamente lee la `StorageClass`. `smoke` crea un namespace temporal,
solicita un volumen pequeño `ReadWriteOnce`, lo monta, escribe y lee un archivo de
prueba y elimina el namespace. Superar el smoke no registra un binding; el
registro sigue siendo una acción explícita y auditada del control plane.
