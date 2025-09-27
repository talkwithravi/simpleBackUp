#!/bin/bash

# Simple Photo Backup System - Testing without iPhone
# This script simulates the complete workflow using curl commands

echo "=== Simple Photo Backup System Test ==="
echo "Testing without iPhone app using curl commands"
echo ""

# Step 1: Check if Mac agent is running
echo "Step 1: Checking Mac agent status..."
curl -s http://192.168.68.104:8081/pair > /dev/null
if [ $? -eq 0 ]; then
    echo "✅ Mac agent is running"
else
    echo "❌ Mac agent is not running. Please start it first:"
