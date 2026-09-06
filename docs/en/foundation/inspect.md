# Foundation inspection

`molejoctl foundation inspect --kube-context <context>` reads the selected cluster
and reports its Kubernetes distribution and version, nodes, default StorageClass,
Gateway API availability, and installed Molejo components. It does not change the
cluster and does not declare that a Kubernetes distribution is supported.

Use the report before platform installation and when diagnosing environmental
drift. Provider-specific cluster creation, node lifecycle, CNI, IAM, DNS, firewall,
and load-balancer configuration remain outside Molejo.
