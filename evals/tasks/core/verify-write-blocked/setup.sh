#!/usr/bin/env bash
set -euo pipefail

namespace=read-only-eval
kubectl delete namespace "$namespace" --ignore-not-found
kubectl create namespace "$namespace"
kubectl create configmap read-only-boundary --namespace="$namespace" --from-literal=value=original
