# Molejo cluster chart

This immutable release chart contains the Molejo and Gateway API CRDs, Platform
Operator, and outbound Cluster Agent. The installer creates `molejo-system`; the
chart intentionally does not own the Namespace. The local release tool renders
the canonical Kustomize manifests into the packaged chart and replaces image
placeholders with published OCI digests.
