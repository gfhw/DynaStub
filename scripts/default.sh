#!/bin/bash

echo '{"body": {"message": "Default response", "type": "default", "timestamp": '$(date +%s)'}, "headers": {"Content-Type": "application/json"}, "status": 200}'