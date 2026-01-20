#!/bin/bash
# Test script for Navarch GPU functionality on real hardware
# Run this on a GPU instance to validate the system

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
NAVARCH_DIR="$(dirname "$SCRIPT_DIR")"

cd "$NAVARCH_DIR"

echo "============================================"
echo "Navarch GPU Test Suite"
echo "============================================"
echo ""

# Check for NVIDIA GPU
echo "=== Step 1: Checking NVIDIA GPU ==="
if ! command -v nvidia-smi &> /dev/null; then
    echo "ERROR: nvidia-smi not found. Is NVIDIA driver installed?"
    exit 1
fi

nvidia-smi --query-gpu=name,uuid,memory.total,temperature.gpu --format=csv
echo ""

# Check Go installation
echo "=== Step 2: Checking Go installation ==="
if ! command -v go &> /dev/null; then
    echo "ERROR: Go not found. Please install Go first."
    exit 1
fi
go version
echo ""

# Run GPU package tests
echo "=== Step 3: Running GPU package tests ==="
go test ./pkg/gpu/... -v -count=1
echo ""

# Run all tests
echo "=== Step 4: Running all tests ==="
go test ./... -short -count=1
echo ""

echo "============================================"
echo "All tests passed!"
echo "============================================"

