# B1 Scalable URL Shortener with Analytics

A Go URL-shortening service built around PostgreSQL, Redis, an asynchronous analytics worker, sliding-window rate limiting, Docker Compose and Nginx.

## Features

- Auto-generated short codes using PostgreSQL `BIGSERIAL` IDs + Base62
- Optional custom aliases with collision handling
- PostgreSQL as the source of truth
- Redis read-through cache on the redirect hot path
- Cache TTL automatically respects link expiry
- Click analytics written asynchronously through a bounded queue + worker
- Sliding-window rate limiting in Redis
- Link expiry and soft deletion
- Health/readiness endpoints
- Docker Compose deployment behind Nginx
- k6 load-test starter script

## Project structure

```text
url_shortener/
├── main.go
├── go.mod
├── Dockerfile
├── docker-compose.yml
├── sql/schema.sql
├── internal/
│   ├── base62/base62.go
│   ├── cache/cache.go
│   ├── analytics/worker.go
│   ├── ratelimit/ratelimit.go
│   └── store/store.go
├── nginx/nginx.conf
├── docs/architecture.md
├── loadtest-performance.js
└── rate-limit-test.js
```

## Run locally on Windows

From the project folder:

```powershell
docker compose down -v
docker compose up -d postgres redis
docker ps
go mod tidy
go run .
```

`down -v` is recommended only for a fresh setup because it deletes the local PostgreSQL/Redis volumes.

Expected server message:

```text
URL shortener listening on :8080
```

## API examples

### Health

```powershell
Invoke-RestMethod http://localhost:8080/health
```

### Create an automatic short URL

```powershell
$body = @{ url = "https://www.google.com" } | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri http://localhost:8080/shorten -ContentType "application/json" -Body $body
```

Example response:

```json
{
  "short_code": "1",
  "short_url": "http://localhost:8080/1",
  "original_url": "https://www.google.com"
}
```

### Custom alias

```powershell
$body = @{ url = "https://example.com"; custom_alias = "docs" } | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri http://localhost:8080/shorten -ContentType "application/json" -Body $body
```

### Redirect

Open the returned `short_url` in a browser. A `GET /1` redirects to the original URL.

### Analytics

After visiting a short URL:

```powershell
Invoke-RestMethod http://localhost:8080/analytics/1
```

### Delete

```powershell
Invoke-RestMethod -Method Delete -Uri http://localhost:8080/delete/1
```

## Full Docker stack

```powershell
docker compose up --build -d
```

Then use:

```text
http://localhost/health
```

## Design

The redirect hot path is:

```text
Client -> Nginx -> Go
                  |
                  +-> Redis GET
                  |     |
                  |     +-> hit -> redirect + enqueue analytics
                  |
                  +-> miss -> PostgreSQL -> Redis SET -> redirect
                                      |
                                      +-> analytics queue -> worker -> PostgreSQL
```

## Load testing

The included `loadtest.js` is a starter k6 script. Run it only after creating a known short code and update the target path in the script.

Record p50, p95 and p99 at the levels required by the project brief, then document the bottleneck and before/after tuning results.
