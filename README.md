# Serverless Platform

A self-hosted platform for running Python, Node.js, and custom container functions on Kubernetes and Knative.

- Scale idle functions to zero and start them on demand.
- Organize functions into workspaces with owner/member roles and separate Kubernetes namespaces.
- Deploy asynchronously, invoke functions through public or authenticated endpoints, and follow deployment events.
- Collect metrics with Prometheus, stream logs from Loki, and export traces through OpenTelemetry.

## How it works

```text
Client → Go API → PostgreSQL (users, workspaces, functions, deployments)
             └→ NATS JetStream → Go worker → Knative (function execution)
```

The API saves deployment requests and returns `202 Accepted`; the worker deploys them to Knative and updates their status. Each workspace uses a `fn-{slug}` namespace.

## Repository

| Path | Purpose |
| --- | --- |
| [cmd/api](cmd/api) | HTTP API and startup configuration |
| [cmd/worker](cmd/worker) | Deployment worker |
| [internal](internal) | Authentication, handlers, storage, deployment, and observability |
| [migrations](migrations) | Versioned PostgreSQL migrations, embedded in the API binary |
| [runtimes](runtimes) | Python and Node.js runtime images |
| [scripts](scripts), [loadtest/k6](loadtest/k6) | Smoke tests and load tests |

## Development

Requires Go 1.25. Running the services also requires PostgreSQL, NATS with JetStream, and access to a Kubernetes cluster with Knative installed.

Set `DATABASE_DSN` and the relevant service endpoints in your environment, then run the API and worker in separate terminals:

```sh
make dev
go run ./cmd/worker
```

The API applies database migrations at startup. Both processes use in-cluster Kubernetes credentials or your local `KUBECONFIG` (falling back to `~/.kube/config`).

```sh
make test       # Unit tests
make test-race  # Tests with the race detector
make lint       # Requires golangci-lint
```

See the [Makefile](Makefile) for additional commands. Manual migration commands use `DATABASE_URL`, while the application uses `DATABASE_DSN`.

## Configuration and deployment

See [internal/config/config.go](internal/config/config.go) for all settings and defaults.

| Variable | Purpose |
| --- | --- |
| `DATABASE_DSN` | PostgreSQL connection string |
| `NATS_URL`, `NATS_TOKEN` | Deployment queue connection and authentication |
| `PLATFORM_NAMESPACE` | Namespace for platform resources; defaults to `serverless-platform` |
| `PUBLIC_BASE_URL` | External API origin used to generate public function URLs |
| `PROMETHEUS_URL`, `LOKI_URL`, `OTLP_ENDPOINT` | Metrics, logs, and trace backends |
| `PORT`, `ENVIRONMENT`, `CORS_ORIGINS` | API port, environment, and allowed browser origins |

GitHub Actions builds container images in GHCR. Kubernetes manifests are maintained separately in `platform-gitops` and deployed through Argo CD. The [Dockerfile](Dockerfile) packages both binaries: `/app/api` is the default entrypoint; the worker deployment runs `/app/worker`.

## API essentials

Log in through `POST /api/v1/auth/login` and send the returned token as `Authorization: Bearer <token>`. Workspace-scoped endpoints use the workspace assigned in the token.

| Endpoint | Purpose |
| --- | --- |
| `/api/v1/functions` | Create and list functions; use `/{name}` to inspect, update, or delete |
| `POST /api/v1/functions/{name}/invoke` | Invoke with authentication |
| `/fn/{workspace}/{name}` | Invoke a public function without authentication |
| `/api/v1/deploys/{id}`, `/api/v1/deploys/{id}/events` | Deployment status and event stream |
| `/api/v1/logs/stream`, `/api/v1/metrics/query_range` | Workspace logs and metrics |
| `/api/v1/users`, `/api/v1/workspaces` | Admin management |

Managed functions supply code and a `python` or `nodejs` runtime. Custom containers use `deploy_type: "byoi"` and an image reference. Containers must listen on port `8080`. See [request models](internal/model/function.go) for validation and [route registration](cmd/api/main.go) for the complete API.
