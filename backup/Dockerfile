# Minimal nightly backup container:
#  - alpine + postgresql-client (for pg_dump) + age (for encryption)
#  - cron driven; settings come from env vars
FROM alpine:3.19

RUN apk add --no-cache \
        postgresql16-client \
        age \
        bash \
        coreutils \
        tzdata \
        gnupg \
        curl \
        mosquitto-clients

COPY backup-now.sh /usr/local/bin/backup-now
COPY entrypoint.sh /usr/local/bin/entrypoint
RUN chmod +x /usr/local/bin/backup-now /usr/local/bin/entrypoint

# Cron file dropped at runtime so the user can override BACKUP_SCHEDULE
# without rebuilding the image.
ENTRYPOINT ["/usr/local/bin/entrypoint"]
