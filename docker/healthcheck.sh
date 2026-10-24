#!/bin/sh
# Docker healthcheck: requests /api/config on the address and base path that
# Dropcatch serves, as configured by DROPCATCH_LISTEN and DROPCATCH_BASE_PATH.
listen="${DROPCATCH_LISTEN:-:8080}"
port="${listen##*:}"
host="${listen%:*}"
case "$host" in
  ""|0.0.0.0|"[::]") host=127.0.0.1 ;;
esac
base="${DROPCATCH_BASE_PATH:-/}"
base="${base#/}"
base="${base%/}"
wget -q --spider "http://$host:$port${base:+/$base}/api/config" || exit 1
