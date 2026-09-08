#!/bin/sh

set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
dockerfile="${repo_root}/Dockerfile"
entrypoint="${repo_root}/docker/docker-entrypoint.sh"

grep -Eq '^RUN apk .* add .*su-exec' "${dockerfile}" || {
    echo "Dockerfile must install su-exec so startup can drop privileges" >&2
    exit 1
}

if grep -Eq '^USER[[:space:]]+1000(:1000)?$' "${dockerfile}"; then
    echo "Image must start as root so a bind-mounted /data can be prepared" >&2
    exit 1
fi

grep -Fq 'chown -R 1000:1000 "${data_path}"' "${entrypoint}" || {
    echo "Entrypoint must make existing /data contents writable by the app user" >&2
    exit 1
}

grep -Fq 'exec su-exec 1000:1000' "${entrypoint}" || {
    echo "Entrypoint must drop privileges before starting the app or a custom command" >&2
    exit 1
}

echo "entrypoint permission checks passed"
