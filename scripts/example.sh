#!/bin/bash

echo '{"body": {"message": "Hello from script!", "timestamp": '$(date +%s)', "env_var": "'$CUSTOM_VAR'"}, "headers": {"Content-Type": "application/json"}, "status": 200}'