FROM alpine:latest

RUN apk --no-cache add ca-certificates bash

# Copy the manager binary directly to root
COPY build/manager /manager

RUN chmod +x /manager

RUN mkdir -p /config

# Copy the entrypoint script
COPY scripts/entrypoint.sh /entrypoint.sh

RUN chmod +x /entrypoint.sh

# Copy TLS certificates
COPY build/certs/tls.crt /etc/tls/tls.crt
COPY build/certs/tls.key /etc/tls/tls.key

RUN chmod 644 /etc/tls/tls.crt && chmod 600 /etc/tls/tls.key

EXPOSE 8080 8443 8081

# Run the entrypoint script which will keep the container running
CMD ["/entrypoint.sh"]
