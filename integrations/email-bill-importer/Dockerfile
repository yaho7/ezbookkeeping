FROM python:3.13-alpine3.22

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1

RUN addgroup -S -g 1000 importer \
    && adduser -S -D -H -u 1000 -G importer importer \
    && mkdir -p /app /data \
    && chown -R importer:importer /app /data

WORKDIR /app
COPY --chown=importer:importer bill_importer /app/bill_importer

USER importer
ENTRYPOINT ["python", "-m", "bill_importer"]
