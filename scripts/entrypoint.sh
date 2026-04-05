#!/bin/sh

set -e

echo "Starting k8s-http-fake-operator..."

# Check if start.sh exists in /config directory
if [ -f "/config/start.sh" ]; then
    echo "Using ConfigMap start.sh script..."
    exec sh /config/start.sh
else
    echo "ConfigMap start.sh not found, using default configuration..."
    
    # Default configuration
    ARGS="--metrics-bind-address=0"
    ARGS="$ARGS --metrics-secure=false"
    ARGS="$ARGS --health-probe-bind-address=:8081"
    ARGS="$ARGS --leader-elect=false"
    ARGS="$ARGS --enable-http2=false"
    ARGS="$ARGS --http-port=8080"
    ARGS="$ARGS --https-port=8443"
    ARGS="$ARGS --tls-cert-file=/etc/tls/tls.crt"
    ARGS="$ARGS --tls-key-file=/etc/tls/tls.key"
    
    echo "Starting manager with default arguments: $ARGS"
    
    # Infinite loop to keep the container running and restart the manager if it crashes
    while true; do
        echo "Starting manager process..."
        /manager $ARGS
        
        # If manager exits, wait a bit before restarting
        echo "Manager process exited with code $?"
        echo "Waiting 5 seconds before restarting..."
        sleep 5
        
        echo "Restarting manager..."
    done
fi