#!/bin/sh

set -e

data_path="${EBK_DATA_PATH:-/data}"
default_conf_path="${EBK_DEFAULT_CONF_PATH:-/ezbookkeeping/conf/ezbookkeeping.ini}"

mkdir -p "${data_path}" "${data_path}/log" "${data_path}/storage"

if [ -z "${EBK_CONF_PATH:-}" ]; then
    EBK_CONF_PATH="${data_path}/ezbookkeeping.ini"
    export EBK_CONF_PATH

    if [ ! -f "${EBK_CONF_PATH}" ]; then
        cp "${default_conf_path}" "${EBK_CONF_PATH}"
    fi
fi

if [ -z "${EBK_DATABASE_DB_PATH:-}" ]; then
    export EBK_DATABASE_DB_PATH="${data_path}/ezbookkeeping.db"
fi
if [ -z "${EBK_LOG_LOG_PATH:-}" ]; then
    export EBK_LOG_LOG_PATH="${data_path}/log/ezbookkeeping.log"
fi
if [ -z "${EBK_STORAGE_LOCAL_FILESYSTEM_PATH:-}" ]; then
    export EBK_STORAGE_LOCAL_FILESYSTEM_PATH="${data_path}/storage"
fi

if [ $# -gt 0 ]; then
    exec "$@"
else
    exec /ezbookkeeping/ezbookkeeping server run "--conf-path=${EBK_CONF_PATH}"
fi
