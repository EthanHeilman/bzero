#!/bin/sh
cd /bctl-agent-files/bctl/agent 
while true; do go run /bctl-agent-files/bctl/agent/agent.go -serviceUrl=$SERVICE_URL; sleep 5; done