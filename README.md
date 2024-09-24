# Audit Log to Traces

This app parses audit log, converts them into traces and sends it to Jaeger-compatible storage. 
Grafana is used as a tool to query and find strange traces.

## Howto

Start grafana stack:
```
podman play k8s grafana-stack.yaml
```

Start the app:
```
go run -mod vendor . --otlp-addr=localhost:4317 --audit-log-path=./data/ip-10-0-31-69.ec2.internal-audit.log
```

Open http://localhost:3000 in browser (default login is `admin`/`admin`) and use Explore view to 
lookup audit log
