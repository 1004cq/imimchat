# Keep the official MinIO server image and add only a tiny HTTP probe binary.
FROM minio/minio:latest
COPY --from=busybox:1.36.1-musl /bin/busybox /usr/local/bin/busybox
