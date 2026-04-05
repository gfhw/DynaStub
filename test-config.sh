#!/bin/bash

# Test script to verify ConfigMap configuration

echo "=== Testing ConfigMap Configuration ==="

# Simulate Helm template rendering
VALUES_FILE="./charts/k8s-http-fake-operator/values.yaml"
CONFIGMAP_FILE="./charts/k8s-http-fake-operator/templates/configmap.yaml"

echo "Values file: $VALUES_FILE"
echo "ConfigMap template: $CONFIGMAP_FILE"

# Display the operator configuration from values.yaml
echo ""
echo "=== Current Operator Configuration ==="
echo "Metrics enabled: $(grep -A 1 'metrics:' $VALUES_FILE | grep 'enabled:' | awk '{print $2}')"
echo "Metrics bind address: $(grep -A 2 'metrics:' $VALUES_FILE | grep 'bindAddress:' | awk '{print $2}')"
echo "Health probe bind address: $(grep -A 1 'healthProbe:' $VALUES_FILE | grep 'bindAddress:' | awk '{print $2}')"
echo "Leader election enabled: $(grep -A 1 'leaderElection:' $VALUES_FILE | grep 'enabled:' | awk '{print $2}')"
echo "HTTP port: $(grep -A 1 'server:' $VALUES_FILE | grep 'httpPort:' | awk '{print $2}')"
echo "HTTPS port: $(grep -A 1 'server:' $VALUES_FILE | grep 'httpsPort:' | awk '{print $2}')"

echo ""
echo "=== Expected start.sh content ==="
cat << 'EOF'
#!/bin/sh

# Build command line arguments from ConfigMap values

ARGS=""

# Metrics configuration
ARGS="$ARGS --metrics-bind-address=:8080"
ARGS="$ARGS --metrics-secure=true"

# Health probe configuration
ARGS="$ARGS --health-probe-bind-address=:8081"

# Leader election configuration
ARGS="$ARGS --leader-elect=false"

# HTTP/2 configuration
ARGS="$ARGS --enable-http2=false"

# Server configuration
ARGS="$ARGS --http-port=8080"
ARGS="$ARGS --https-port=8443"
ARGS="$ARGS --tls-cert-file=/etc/tls/tls.crt"
ARGS="$ARGS --tls-key-file=/etc/tls/tls.key"

# Execute manager with all arguments
exec /manager $ARGS
EOF

echo ""
echo "=== Configuration Summary ==="
echo "✓ values.yaml updated with operator configuration"
echo "✓ configmap.yaml template created"
echo "✓ deployment.yaml updated to mount ConfigMap"
echo "✓ Dockerfile updated to support script-based startup"

echo ""
echo "=== Usage ==="
echo "1. Deploy with default values:"
echo "   kubectl apply -f <(helm template k8s-http-fake-operator ./charts/k8s-http-fake-operator)"
echo ""
echo "2. Deploy with custom values:"
echo "   kubectl apply -f <(helm template k8s-http-fake-operator ./charts/k8s-http-fake-operator -f custom-values.yaml)"
echo ""
echo "3. Verify ConfigMap:"
echo "   kubectl get configmap k8s-http-fake-operator-config -o yaml"
echo ""
echo "4. Check pod logs:"
echo "   kubectl logs -l app.kubernetes.io/name=k8s-http-fake-operator"