#!/bin/bash

DELAY_SECONDS=${1:-5}

echo "Sleeping for $DELAY_SECONDS seconds..."
sleep $DELAY_SECONDS

echo '{"body": {"message": "Delayed response", "delay": "'$DELAY_SECONDS'", "timestamp": '$(date +%s)'}, "headers": {"Content-Type": "application/json"}, "status": 200}'