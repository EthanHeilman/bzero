#!/bin/sh
cd /bctl-agent-files/bctl/agent 
while true; do go run . -serviceUrl=$SERVICE_URL; sleep 5; done