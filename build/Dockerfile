FROM alpine:latest

RUN apk --no-cache add ca-certificates bash

# Copy the manager binary directly to root
COPY build/manager /manager

RUN chmod +x /manager

RUN mkdir -p /config

# Copy the entrypoint script
COPY scripts/entrypoint.sh /entrypoint.sh

RUN chmod +x /entrypoint.sh

# Create webhook certificates directory
RUN mkdir -p /tmp/k8s-webhook-server/serving-certs

# Copy TLS certificates to webhook directory
COPY build/certs/tls.crt /tmp/k8s-webhook-server/serving-certs/tls.crt
COPY build/certs/tls.key /tmp/k8s-webhook-server/serving-certs/tls.key

RUN chmod 644 /tmp/k8s-webhook-server/serving-certs/tls.crt && chmod 600 /tmp/k8s-webhook-server/serving-certs/tls.key

EXPOSE 9443 8081

# Run the entrypoint script which will keep the container running
CMD ["/entrypoint.sh"]
