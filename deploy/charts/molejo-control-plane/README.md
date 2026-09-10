# Molejo control plane chart

This chart is installed by `molejoctl platform control-plane install`. It contains only
the in-cluster PostgreSQL database, bootstrap Job, control-plane API, and the
internal HTTPS and mTLS services used by the cluster Agent.

The installer creates the required credentials and certificate Secrets before
installing the chart. When `postgresql.storageClass` is empty, the PVC uses the
cluster's default StorageClass.

Public access is disabled by default. `molejoctl platform control-plane install
--public-host <host>` enables one `HTTPRoute` that sends all console traffic to
`console-web`; the console remains the only public backend and proxies `/api/*`
to the internal API Service.
