ui = false
disable_mlock = true
api_addr = "https://openbao.molejo-secrets.svc:8200"
cluster_addr = "https://openbao-0.openbao-internal.molejo-secrets.svc:8201"
storage "raft" {
  path = "/openbao/data"
  node_id = "openbao-0"
}
listener "tcp" {
  address = "0.0.0.0:8200"
  cluster_address = "0.0.0.0:8201"
  tls_cert_file = "/openbao/tls/tls.crt"
  tls_key_file = "/openbao/tls/tls.key"
  tls_min_version = "tls12"
}
