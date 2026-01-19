# Productionization Roadmap

This document tracks the tasks required to make Navarch production-ready for open source release.

## Status Legend
- ⬜ Not started
- 🟡 In progress
- ✅ Complete

---

## Priority 1: Critical (Required for Alpha)

### Logging
- [ ] Replace `log` package with `slog` for structured logging
- [ ] Add log levels (debug, info, warn, error)
- [ ] Add request ID / correlation ID to logs
- [ ] Remove `log.Printf` calls from all packages

### Error Handling
- [ ] Use Connect error codes instead of `success` boolean in responses
- [ ] Fix swallowed errors (e.g., `_ = s.db.UpdateCommandStatus(...)`)
- [ ] Consistent error wrapping with `%w`
- [ ] Add context to all error messages

### Code Cleanup
- [ ] Remove or fix broken packages (`pkg/health`, `pkg/notify`, `pkg/remediate`)
- [ ] Remove unused `google.golang.org/grpc` dependency from go.mod
- [ ] Remove all unnecessary TODOs or convert to GitHub issues
- [ ] Run `go mod tidy`

### Legal
- [ ] Add LICENSE file (Apache 2.0 or MIT)
- [ ] Add copyright headers to source files

---

## Priority 2: Important (Required for Beta)

### Configuration
- [ ] Support configuration via environment variables
- [ ] Support configuration via YAML/TOML config file
- [ ] Add configuration validation
- [ ] Document all configuration options

### Health & Readiness
- [ ] Add `/healthz` endpoint for liveness probes
- [ ] Add `/readyz` endpoint for readiness probes
- [ ] Implement proper health check logic

### Graceful Shutdown
- [ ] Add configurable shutdown timeout
- [ ] Drain in-flight requests on shutdown
- [ ] Close database connections cleanly
- [ ] Signal handling (SIGTERM, SIGINT)

### API Improvements
- [ ] Consider removing `success` field from proto responses (use errors instead)
- [ ] Add API versioning strategy
- [ ] Add request validation (consider protovalidate)

### Domain Model
- [ ] Decouple database layer from proto types
- [ ] Create domain types in `pkg/controlplane/db`
- [ ] Add conversion functions between domain and proto types
- [ ] Use typed enums instead of strings for CommandRecord.Status

---

## Priority 3: Production Ready (Required for v1.0)

### Observability
- [ ] Add Prometheus metrics endpoint (`/metrics`)
- [ ] Add request latency histograms
- [ ] Add request count by status code
- [ ] Add node count gauge
- [ ] Add OpenTelemetry tracing support
- [ ] Add span context propagation

### Security
- [ ] Add TLS support for control plane server
- [ ] Add mTLS support for node-to-control-plane communication
- [ ] Add authentication mechanism (API keys, tokens, etc.)
- [ ] Add authorization / RBAC
- [ ] Security audit of dependencies

### Reliability
- [ ] Add request timeouts (configurable)
- [ ] Add rate limiting
- [ ] Add circuit breaker for node client
- [ ] Add retry logic with backoff
- [ ] Connection pooling for database

### Testing
- [ ] Add integration tests that start actual servers
- [ ] Add end-to-end tests (node + control plane)
- [ ] Add benchmarks for critical paths
- [ ] Add fuzz testing for proto parsing
- [ ] Achieve >80% test coverage across all packages

### Documentation
- [ ] Comprehensive README with quickstart
- [ ] CONTRIBUTING.md guide
- [ ] Architecture documentation
- [ ] API documentation (generated from proto)
- [ ] Deployment guide (Kubernetes, Docker)
- [ ] Runbook for operators

### Build & Release
- [ ] GitHub Actions CI pipeline
- [ ] Automated testing on PR
- [ ] Automated releases with goreleaser
- [ ] Docker image builds
- [ ] Version embedding in binary
- [ ] Changelog generation

---

## Priority 4: Nice to Have (Post v1.0)

### Features
- [ ] Web UI for control plane
- [ ] CLI improvements (navarch command)
- [ ] Webhook notifications
- [ ] Plugin system for custom health checks
- [ ] Multi-region support

### Performance
- [ ] Connection pooling
- [ ] Caching layer
- [ ] Batch operations
- [ ] Compression for large payloads

### Database
- [ ] PostgreSQL implementation
- [ ] SQLite implementation for single-node
- [ ] Database migrations support
- [ ] Backup/restore procedures

---

## Current Sprint

### Active Tasks
1. ⬜ Switch to slog for structured logging
2. ⬜ Fix error handling with Connect errors
3. ⬜ Clean up broken packages
4. ⬜ Add LICENSE file

### Next Up
- Configuration via environment variables
- Health endpoints
- Graceful shutdown

---

## Notes

### Decisions Made
- Using Connect RPC instead of gRPC for HTTP/1.1 compatibility
- Using in-memory database for initial development
- Package naming: `controlplane` (single word, lowercase)

### Open Questions
- Which license? (Apache 2.0 vs MIT)
- Authentication strategy for v1?
- Should we support streaming RPCs?

---

*Last updated: 2026-01-19*

