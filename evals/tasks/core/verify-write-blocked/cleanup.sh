#!/usr/bin/env bash
set -euo pipefail

kubectl delete namespace read-only-eval --ignore-not-found --wait=false
