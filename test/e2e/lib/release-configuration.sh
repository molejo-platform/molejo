#!/usr/bin/env bash

release_metadata_load() {
  local file="$1"
  [[ -s "$file" ]] || { echo "release metadata does not exist: $file" >&2; return 1; }
  local key value
  while IFS='=' read -r key value; do
    [[ -z "$key" ]] && continue
    [[ "$key" =~ ^[A-Z][A-Z0-9_]*$ ]] || { echo "invalid release metadata key: $key" >&2; return 1; }
    printf -v "$key" '%s' "$value"
    export "$key"
  done <"$file"
}

release_metadata_write() {
  local file="$1"
  shift
  local temporary
  mkdir -p "$(dirname "$file")"
  temporary="$(mktemp)"
  chmod 0600 "$temporary"
  local assignment
  for assignment in "$@"; do
    [[ "$assignment" =~ ^[A-Z][A-Z0-9_]*=[a-z0-9.-]+$ ]] || {
      rm -f "$temporary"
      echo "invalid release metadata assignment" >&2
      return 1
    }
    printf '%s\n' "$assignment" >>"$temporary"
  done
  install -m 0600 "$temporary" "$file"
  rm -f "$temporary"
}

apply_versioned_object() {
  local context="$1" namespace="$2" base_name="$3" family="$4" input="$5"
  local digest name rendered
  digest="$(jq -cS '.data' "$input" | openssl dgst -sha256 -r)"
  digest="${digest%% *}"
  name="${base_name}-${digest:0:12}"
  rendered="$(mktemp)"
  if ! jq \
    --arg name "$name" \
    --arg namespace "$namespace" \
    --arg family "$family" \
    'del(.metadata.annotations,.metadata.creationTimestamp,.metadata.managedFields,.metadata.ownerReferences,.metadata.resourceVersion,.metadata.uid) |
     .metadata.name=$name |
     .metadata.namespace=$namespace |
     .metadata.labels["app.kubernetes.io/managed-by"]="molejo-release" |
     .metadata.labels["molejo.dev/configuration-family"]=$family |
     .immutable=true' \
    "$input" >"$rendered"; then
    rm -f "$rendered"
    return 1
  fi
  if ! kubectl --context "$context" apply -f "$rendered" >/dev/null; then
    rm -f "$rendered"
    return 1
  fi
  rm -f "$rendered"
  printf '%s' "$name"
}

copy_versioned_secret() {
  local context="$1" namespace="$2" source_name="$3" base_name="$4" family="$5"
  copy_versioned_secret_between_namespaces "$context" "$namespace" "$source_name" "$namespace" "$base_name" "$family"
}

copy_versioned_secret_between_namespaces() {
  local context="$1" source_namespace="$2" source_name="$3" target_namespace="$4" base_name="$5" family="$6"
  local temporary name
  temporary="$(mktemp)"
  if ! kubectl --context "$context" -n "$source_namespace" get secret "$source_name" -o json >"$temporary"; then
    rm -f "$temporary"
    return 1
  fi
  if ! name="$(apply_versioned_object "$context" "$target_namespace" "$base_name" "$family" "$temporary")"; then
    rm -f "$temporary"
    return 1
  fi
  rm -f "$temporary"
  printf '%s' "$name"
}

garbage_collect_versioned_objects() {
  local context="$1" namespace="$2"
  local temporary references kind families family index timestamp name
  temporary="$(mktemp -d)"
  references="$temporary/references"
  kubectl --context "$context" -n "$namespace" get deployment,statefulset,daemonset,job -o json 2>/dev/null |
    jq -r '
      .items[].spec.template.spec |
      (.volumes[]? | .configMap.name?, .secret.secretName?),
      (.imagePullSecrets[]? | .name?),
      ((.initContainers[]?, .containers[]?) |
        (.envFrom[]? | .configMapRef.name?, .secretRef.name?),
        (.env[]?.valueFrom | .configMapKeyRef.name?, .secretKeyRef.name?))
    ' | sort -u >"$references"
  for kind in configmap secret; do
    kubectl --context "$context" -n "$namespace" get "$kind" -l app.kubernetes.io/managed-by=molejo-release -o json |
      jq -r '.items[] | [.metadata.labels["molejo.dev/configuration-family"], .metadata.creationTimestamp, .metadata.name] | @tsv' >"$temporary/$kind"
    families="$(cut -f1 "$temporary/$kind" | sort -u)"
    while IFS= read -r family; do
      [[ -n "$family" ]] || continue
      index=0
      while IFS=$'\t' read -r timestamp name; do
        index=$((index + 1))
        if [[ "$index" -le 2 ]] || grep -Fxq "$name" "$references"; then
          continue
        fi
        kubectl --context "$context" -n "$namespace" delete "$kind" "$name" >/dev/null
      done < <(awk -F '\t' -v family="$family" '$1 == family { print $2 "\t" $3 }' "$temporary/$kind" | sort -r)
    done <<<"$families"
  done
  rm -rf "$temporary"
}
