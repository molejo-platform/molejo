# Kubernetes storage

Molejo does not install or manage a storage provisioner. The cluster operator
selects a `StorageClass`, proves it locally, and then registers the binding in the
installation administration page.

```shell
molejoctl capability storage verify \
  --kube-context my-cluster \
  --storage-class standard

molejoctl capability storage smoke \
  --kube-context my-cluster \
  --storage-class standard
```

`verify` only reads the `StorageClass`. `smoke` creates a temporary namespace,
requests a small `ReadWriteOnce` volume, mounts it, writes and reads a probe file,
then removes the namespace. Passing the smoke test does not register a binding;
registration remains an explicit and audited control-plane action.
