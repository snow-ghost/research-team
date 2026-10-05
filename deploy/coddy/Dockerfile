FROM debian:bookworm-slim@sha256:3783cc01769c7b2b1b83a5c5ad96c815348e28ed7da68e2e3687004faa906251
COPY --from=ca-bundle /ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
# The reviewed static agent binary is mounted read-only at runtime.
ENTRYPOINT ["/usr/local/bin/coddy"]
