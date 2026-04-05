# Scripts

This directory contains utility scripts for the k8s-http-fake-operator.

## Available Scripts

### entrypoint.sh
Container entrypoint script that:
1. Checks if ConfigMap start.sh exists and uses it
2. Falls back to default configuration if ConfigMap not found
3. Runs manager in an infinite loop to keep container running
4. Automatically restarts manager if it crashes

## Usage

The entrypoint script is automatically used when the container starts. It will:

1. **With ConfigMap**: Use the start.sh script from the ConfigMap mounted at `/config/start.sh`
2. **Without ConfigMap**: Use default configuration values

## Default Configuration

When no ConfigMap is provided, the following defaults are used:
- HTTP Port: 8080
- HTTPS Port: 8443
- Health Probe Port: 8081
- Metrics: Disabled
- Leader Election: Disabled
- HTTP/2: Disabled

## Container Lifecycle

The entrypoint script ensures the container stays running by:
1. Starting the manager process
2. Monitoring the process
3. Restarting automatically if it crashes (with 5-second delay)
4. Providing detailed logging for debugging

## Testing Locally

You can test the entrypoint script locally:

```bash
# Build the image
docker build -t k8s-http-fake-operator:latest .

# Run the container
docker run -p 8080:8080 -p 8443:8443 -p 8081:8081 k8s-http-fake-operator:latest

# Test the health endpoint
curl http://localhost:8081/healthz
```

## Custom Configuration

To use custom configuration, provide a ConfigMap with a `start.sh` script:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: custom-config
data:
  start.sh: |
    #!/bin/sh
    ARGS="--http-port=9090 --https-port=9443"
    while true; do
        /manager $ARGS
        sleep 5
    done
```

Then mount it in your deployment:

```yaml
volumes:
  - name: config
    configMap:
      name: custom-config

volumeMounts:
  - name: config
    mountPath: /config
```

## Troubleshooting

### Container exits immediately
Check the logs:
```bash
docker logs <container-id>
```

### Manager keeps restarting
This is expected behavior if the manager crashes. The script will automatically restart it. Check logs for the root cause.

### ConfigMap not working
Ensure the ConfigMap is properly mounted at `/config/start.sh`:
```bash
kubectl exec -it <pod-name> -- cat /config/start.sh
```