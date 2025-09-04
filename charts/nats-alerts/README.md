# NATS Alerts Helm Chart

A Helm chart for deploying NATS JetStream alerting rules to Prometheus using PrometheusRule CRDs.

## Overview

This chart creates Prometheus alerting rules for monitoring NATS JetStream instances. It monitors critical metrics including:

- **Resource Utilization**: Memory and storage usage
- **Consumer Health**: Pending messages, ack pending, stalled consumers
- **Stream Performance**: Message counts, stream size, ingestion rates
- **Connectivity**: Connection drops and high connection counts
- **Consumer Management**: Consumer drops and high consumer counts

## Prerequisites

- Kubernetes cluster with Prometheus Operator installed
- NATS server with Prometheus exporter (sidecar or standalone)
- Helm 3.x

## Installation

### Basic Installation

```bash
# Install with default values
helm install nats-alerts ./charts/nats-alerts

# Install in specific namespace
helm install nats-alerts ./charts/nats-alerts -n monitoring
```

### Custom Installation

```bash
# Install with custom values
helm install nats-alerts ./charts/nats-alerts \
  --set form.alert.enabled=critical \
  --set form.alert.groups.jetstream.rules.natsJetStreamHighMemoryUsage.val=85 \
  -n monitoring
```

### Using Values File

Create a `my-values.yaml` file:

```yaml
form:
  alert:
    enabled: warning
    labels:
      release: kube-prometheus-stack
    groups:
      jetstream:
        enabled: critical
        rules:
          natsJetStreamHighMemoryUsage:
            val: 85  # Lower threshold to 85%
          natsJetStreamHighPendingMessages:
            val: 3000  # Custom critical threshold
      connectivity:
        enabled: warning
```

Then install:

```bash
helm install nats-alerts ./charts/nats-alerts -f my-values.yaml -n monitoring
```

## Configuration

### Alert Levels

The chart supports hierarchical alert enabling:

- `critical`: Only critical severity alerts
- `warning`: Critical and warning alerts  
- `info`: All alerts

### Key Configuration Options

| Parameter | Description | Default |
|-----------|-------------|---------|
| `form.alert.enabled` | Global alert level | `warning` |
| `form.alert.labels.release` | Prometheus operator release label | `kube-prometheus-stack` |
| `form.alert.groups.jetstream.enabled` | JetStream alerts level | `warning` |
| `form.alert.groups.connectivity.enabled` | Connectivity alerts level | `warning` |
| `form.alert.groups.consumers.enabled` | Consumer alerts level | `warning` |

### JetStream Alert Thresholds

| Alert | Default Threshold | Severity | Duration |
|-------|------------------|----------|----------|
| High Memory Usage | 90% | critical | 5m |
| High Storage Usage | 90% | critical | 5m |
| High Pending Messages (Warning) | 2,000 | warning | 10m |
| High Pending Messages (Critical) | 5,000 | critical | 10m |
| High Ack Pending | 2,000 | critical | 10m |
| Consumer Stalled | Rate = 0 | warning | 15m |
| High Message Count | 1,000,000 | warning | 15m |
| High Stream Size | 10GB | warning | 15m |
| No Ingestion | Rate = 0 | warning | 15m |

### Connectivity Alert Thresholds

| Alert | Default Threshold | Severity | Duration |
|-------|------------------|----------|----------|
| Sudden Connection Drop | >5 changes + negative trend | warning | 5m |
| High Active Connections | >1,000 | warning | 5m |

### Consumer Management Alert Thresholds

| Alert | Default Threshold | Severity | Duration |
|-------|------------------|----------|----------|
| Sudden Consumer Drop | >2 changes + negative trend | warning | 5m |
| High Total Consumers | >100 | warning | 5m |

## Important Notes

### Job Name Pattern

The alerts expect Prometheus metrics from a job named `{release-name}-stats`. For example:
- If you install as `helm install my-nats-alerts`, it looks for job `my-nats-alerts-stats`
- This should match your NATS exporter's job configuration in Prometheus

### Namespace Targeting

The alerts target NATS metrics in the same namespace where you deploy this chart. If your NATS is in a different namespace, you'll need to:

1. Deploy the chart in the same namespace as your NATS instance, OR
2. Customize the alert expressions to remove the namespace filter

### Prometheus Operator Labels

Ensure the `form.alert.labels.release` matches your Prometheus Operator's selector. Common values:
- `kube-prometheus-stack` (default)
- `prometheus`
- `prometheus-operator`

## Examples

### Monitor NATS in Different Namespace

If your NATS is in namespace `ace` but you want alerts in `monitoring`:

```bash
# Option 1: Deploy alerts in same namespace as NATS
helm install nats-alerts ./charts/nats-alerts -n ace

# Option 2: Customize namespace filter (advanced)
# Edit values.yaml to remove/modify namespace filters in expressions
```

### High-Sensitivity Environment

For environments requiring stricter monitoring:

```yaml
form:
  alert:
    enabled: warning
    groups:
      jetstream:
        rules:
          natsJetStreamHighMemoryUsage:
            val: 80  # Alert at 80% instead of 90%
          natsJetStreamHighStorageUsage:
            val: 80
          natsJetStreamHighPendingMessagesWarning:
            val: 1000  # Warning at 1,000 instead of 2,000
          natsJetStreamHighPendingMessages:
            val: 3000  # Critical at 3,000 instead of 5,000
```

### Production Deployment

```bash
# Install with production-ready settings
helm install nats-alerts ./charts/nats-alerts \
  --set form.alert.enabled=critical \
  --set form.alert.labels.release=kube-prometheus-stack \
  --namespace monitoring \
  --create-namespace
```

## Verification

Check if the PrometheusRule was created:

```bash
# List PrometheusRules
kubectl get prometheusrule -n monitoring

# Check the rule content
kubectl get prometheusrule nats-alerts -n monitoring -o yaml
```

Verify alerts are loaded in Prometheus:
1. Open Prometheus UI
2. Go to Status → Rules
3. Look for rules starting with `nats.jetstream`, `nats.connectivity`, `nats.consumers`

## Troubleshooting

### Alerts Not Appearing

1. **Check PrometheusRule exists**: `kubectl get prometheusrule -n <namespace>`
2. **Verify Prometheus Operator labels**: Ensure `form.alert.labels.release` matches your Prometheus Operator configuration
3. **Check job name**: Verify your NATS exporter job name matches the pattern `{release-name}-stats`

### No Metrics Found

1. **Check NATS exporter**: Ensure Prometheus is scraping your NATS exporter
2. **Verify namespace**: Make sure chart is deployed in same namespace as NATS or adjust expressions
3. **Check metric names**: Confirm your exporter uses `nats_` prefix for metrics

### Alerts Always Firing/Never Firing

1. **Check thresholds**: Adjust values in `values.yaml` based on your NATS usage patterns
2. **Verify metric availability**: Some metrics only exist when JetStream features are actively used
3. **Review durations**: Adjust `duration` values if alerts are too sensitive or not sensitive enough

## Uninstallation

```bash
helm uninstall nats-alerts -n monitoring
```

This removes the PrometheusRule and stops all associated alerts.