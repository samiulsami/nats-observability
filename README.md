# NATS Observability & Load Testing

Repository containing Grafana dashboards for NATS monitoring and a Go-based load testing tool.

## Contents

This repository contains:
- **Grafana Dashboards**: JetStream and NATS server monitoring dashboards
- **Load Testing Tool**: Go service for generating NATS JetStream load

## Load Testing Tool

A Go-based load testing tool for NATS JetStream that simulates thousands of producers and consumers.

### Features

- **JetStream Support**: Uses NATS JetStream for persistent messaging
- **Configurable Load**: Support for fast and slow consumers with customizable delays
- **Kubernetes Ready**: Includes deployment manifests for running in cluster
- **Real-time Stats**: Reports throughput and error rates every 10 seconds
- **Cobra CLI**: Clean command-line interface with comprehensive flags

### Quick Start

#### Local Development

```bash
# Build the application
go build -o loadtest ./cmd/loadtest

# Run with default settings
./loadtest

# Run with custom parameters
./loadtest --producers=500 --consumers=300 --slow-consumers=100 --rate=50 --consumer-delay=500ms
```

#### Docker Build

```bash
# Build Docker image
docker build -t nats-loadtest:latest .
```

#### Kubernetes Deployment

```bash
# Deploy to ace namespace (connects to ace-nats cluster)
kubectl apply -f k8s/deployment.yaml

# Check logs
kubectl logs -f deployment/nats-loadtest -n ace

# Scale the deployment for more load
kubectl scale deployment nats-loadtest --replicas=10 -n ace
```

### Configuration

**Command Line Flags:**
- `--nats-url`: NATS server URL (default: `nats://ace-nats.ace.svc.cluster.local:4222`)
- `--producers`: Number of producer goroutines (default: 50)
- `--consumers`: Number of fast consumer goroutines (default: 50)  
- `--slow-consumers`: Number of slow consumer goroutines (default: 20)
- `--consumer-delay`: Processing delay for slow consumers (default: 100ms)
- `--rate`: Messages per second per producer (default: 100)
- `--size`: Message size in bytes (default: 1024)
- `--subject`: Subject prefix for messages (default: "loadtest")

**Current Deployment Config (5 replicas × 200 producers × 20 msg/s = 20,000 msg/s total)**

### Architecture

- **Producers**: Publish messages at configured rate using JetStream
- **Fast Consumers**: Pull and immediately acknowledge messages
- **Slow Consumers**: Add processing delay before acknowledging messages  
- **Stream**: Uses work queue retention policy for load balancing
- **Subjects**: Uses pattern `{subject}.producer.{id}` for message routing

## Grafana Dashboards

Use the dashboards in the `grafana/` directory for:
- NATS server monitoring and observability
- JetStream metrics visualization  
- Core NATS metrics tracking
