# Architecture

## Components

- **Go API:** request validation, short-code creation, redirects, analytics API and deletion.
- **PostgreSQL:** durable source of truth for URLs and click events.
- **Redis:** redirect cache and sliding-window rate-limit state.
- **Analytics worker:** consumes a bounded in-process queue so redirects do not wait for an analytics INSERT.
- **Nginx:** reverse proxy in the full Docker deployment.

## Short-code generation

For a normal URL, PostgreSQL allocates a unique `BIGSERIAL` ID. The Go service converts that ID to Base62. Example: ID `1` becomes `1`, while larger IDs use digits plus upper/lowercase letters.

Custom aliases bypass Base62 and are stored directly after validation. PostgreSQL's unique constraint prevents duplicate aliases.

## Redirect path

1. Rate-limit the request using Redis.
2. Check `url:{short_code}` in Redis.
3. On a hit, redirect immediately and enqueue analytics.
4. On a miss, read PostgreSQL.
5. Reject expired/deleted links.
6. Cache the URL, with a TTL capped by the remaining expiry time.
7. Redirect and enqueue analytics.

## Analytics

Analytics events contain the short code, IP, user-agent and referer. They enter a bounded channel and are persisted by a background worker. If the queue is full, an event is dropped rather than making the redirect request wait.

## Rate limiting

Redis sorted sets store timestamps for each API key (or client IP when no API key is supplied). Old timestamps are removed on every request, producing a sliding one-minute window with a 60-request limit.

## Scaling notes

PostgreSQL remains the source of truth. Redis absorbs repeated redirect reads. For a multi-instance production deployment, the analytics queue should be replaced or backed by a shared broker such as Redis Streams, RabbitMQ or Kafka so events are not lost when an application instance stops.
