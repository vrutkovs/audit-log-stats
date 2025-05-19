# Audit Log Metrics

This app parses Kubernetes audit log files and sends them to VictoriaMetrics. A Grafana dashboard is used to render statistics derived from these logs, helping to identify noisy applications or requests that take too much time.

See [Kubernetes documentation](https://kubernetes.io/docs/tasks/debug/debug-cluster/audit/) for more information on enabling audit logs and locating them.

## Features

- Parses Kubernetes audit logs (supports `.log` and `.gz` formats).
- Sends parsed metrics to VictoriaMetrics via Loki-compatible endpoints.
- Provides a Grafana dashboard for visualizing metrics.
- Supports fetching audit logs directly from OpenShift CI Prow jobs.

## Prerequisites

- Go (1.22.7 or later)
- Podman or Docker (for running the Grafana stack)
- Kubernetes audit logs or link to OpenShift CI Prow job

## Installation

Clone the repository:
  ```bash
  git clone https://github.com/vrutkovs/audit-span.git
  cd audit-span
  ```

## Usage

### Start the Grafana Stack

Run the following command to start the Grafana stack:
```bash
podman play kube grafana-stack.yaml
```

This will start Grafana and VictoriaMetrics Logs on their respective ports.

### Parse Audit Logs

To parse audit logs from a directory:
```bash
go run -mod vendor . --audit-log-dir=/path/to/audit-logs
```

To fetch and parse audit logs from an OpenShift CI Prow job:
```bash
go run -mod vendor . --prow-job=https://prow.ci.openshift.org/view/gs/test-platform-results/logs/periodic-ci-openshift-release-master-ci-4.17-e2e-azure-ovn-upgrade/1835770305428066304
```

### Access the Grafana Dashboard

Open your browser and navigate to [http://localhost:3000](http://localhost:3000). The default login credentials are:
- Username: `admin`
- Password: `admin`

Locate the "Audit Log" dashboard to view metrics.

## Configuration

### Grafana Stack

The `grafana-stack.yaml` file defines the deployment for Grafana and VictoriaMetrics Logs. You can customize it as needed, such as changing ports or volume paths.

### Datasources and Dashboards

- Datasources are configured in `grafana/provisioning/datasources/vlogs.yaml`.
- Dashboards are provisioned from `grafana/dashboards`.

### Application Flags

- `--audit-log-dir`: Path to the directory containing audit logs.
- `--prow-job`: URL of the OpenShift CI Prow job to fetch logs from.
- `--loki-addr`: URL to push logs to (default: `http://localhost:9428/insert/loki/api/v1/push`).

### Debugging

Enable debug mode by passing the `--debug` flag when running the application.

## License

This project is licensed under the [MIT License](LICENSE).
