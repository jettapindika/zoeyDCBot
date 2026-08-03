#!/bin/bash
# Deploy script for zoeydcbot after build
set -e

export PATH=$PATH:/usr/local/go/bin
cd /home/ubuntu/project/zoeyDCBot

echo "=== Building ==="
go build -o zoeydcbot ./cmd/zoeydcbot
echo "Build OK"

echo "=== Testing ==="
go test ./...
echo "Tests OK"

echo "=== Deploying ==="
sudo systemctl restart zoeydcbot
echo "Service restarted"

echo "=== Waiting for bot to come online ==="
sleep 5

echo "=== Checking logs ==="
sudo journalctl -u zoeydcbot -n 20 --no-pager -o cat

echo "=== Done ==="
