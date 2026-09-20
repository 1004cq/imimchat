# Official MinIO images live on Quay (Docker Hub minio/minio is gone).
# Add only a tiny HTTP probe binary for the compose healthcheck.
FROM quay.io/minio/minio:latest
COPY --from=busybox:1.36.1-musl /bin/busybox /usr/local/bin/busybox
