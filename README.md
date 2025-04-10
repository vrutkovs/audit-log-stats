# Audit Log metrics

This app parses kubernetes audit log files and sends them to VictoriaMetrics. Grafana dashboard is rendering stats derived from those to find noisy apps or requests taking too much time.

See [k8s documentation](https://kubernetes.io/docs/tasks/debug/debug-cluster/audit/) for more information on how to enable audit logs and where to find them.

## Howto

Start grafana stack:
```
podman play kube grafana-stack.yaml
```

Fetch audit logs archive, extract them and start the app:
```
go run -mod vendor . --audit-log-dir=/tmp/audit-files
```

Open http://localhost:3000 in browser (default login is `admin`/`admin`) and open "Audit Log" dashboard

Apart from extracted audit logs the app can fetch audit logs from Openshift CI:
```
go run -mod vendor . --prow-job=https://prow.ci.openshift.org/view/gs/test-platform-results/logs/periodic-ci-openshift-release-master-ci-4.17-e2e-azure-ovn-upgrade/1835770305428066304
```
