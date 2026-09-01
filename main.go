package main

import (
    "context"
    "encoding/json"
    "errors"
    "log"
    "net"
    "net/http"
    "os"
    "regexp"
    "strings"
    "time"

    "url-shortener/internal/analytics"
    "url-shortener/internal/base62"
    "url-shortener/internal/cache"
    "url-shortener/internal/ratelimit"
    "url-shortener/internal/store"

    "github.com/jackc/pgx/v5/pgxpool"
    "github.com/redis/go-redis/v9"
)

type App struct {
    store     *store.Store
    cache     *cache.Cache
    limiter   *ratelimit.Limiter
    analytics *analytics.Worker
    baseURL   string
}

type shortenRequest struct {
    URL         string     `json:"url"`
    CustomAlias string     `json:"custom_alias,omitempty"`
    ExpiresAt   *time.Time `json:"expires_at,omitempty"`
}

type shortenResponse struct {
    ShortCode string     `json:"short_code"`
    ShortURL  string     `json:"short_url"`
    Original  string     `json:"original_url"`
    ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

var aliasPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func main() {
    ctx := context.Background()

    dbURL := getenv("DATABASE_URL", "postgres://postgres:postgres@127.0.0.1:5432/urlshortener?sslmode=disable")
    redisAddr := getenv("REDIS_ADDR", "127.0.0.1:6379")
    port := getenv("PORT", "8080")
    baseURL := strings.TrimRight(getenv("BASE_URL", "http://localhost:8080"), "/")

    dbCfg, err := pgxpool.ParseConfig(dbURL)
    if err != nil {
        log.Fatal("invalid DATABASE_URL: ", err)
    }
    dbCfg.MaxConns = 20
    dbCfg.MinConns = 2

    db, err := pgxpool.NewWithConfig(ctx, dbCfg)
    if err != nil {
        log.Fatal("database pool: ", err)
    }
    defer db.Close()

    pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()
    if err := db.Ping(pingCtx); err != nil {
        log.Fatal("database: ", err)
    }

    rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
    defer rdb.Close()
    if err := rdb.Ping(pingCtx).Err(); err != nil {
        log.Fatal("redis: ", err)
    }

    st := store.New(db)
    c := cache.New(rdb, 10*time.Minute)
    limiter := ratelimit.New(rdb, 60, time.Minute)
    worker := analytics.NewWorker(st, 256)
    worker.Start(ctx)
    defer worker.Stop()

    app := &App{store: st, cache: c, limiter: limiter, analytics: worker, baseURL: baseURL}

    mux := http.NewServeMux()
    mux.HandleFunc("/", app.redirect)
    mux.HandleFunc("/health", app.health)
    mux.HandleFunc("/ready", app.ready)
    mux.HandleFunc("/shorten", app.shorten)
    mux.HandleFunc("/analytics/", app.analyticsHandler)
    mux.HandleFunc("/delete/", app.deleteURL)

    handler := logging(rateLimitMiddleware(app, mux))
    server := &http.Server{
        Addr:              ":" + port,
        Handler:           handler,
        ReadHeaderTimeout: 5 * time.Second,
        ReadTimeout:       10 * time.Second,
        WriteTimeout:      10 * time.Second,
        IdleTimeout:       60 * time.Second,
    }

    log.Printf("URL shortener listening on :%s", port)
    log.Fatal(server.ListenAndServe())
}

func (a *App) health(w http.ResponseWriter, r *http.Request) {
    writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *App) ready(w http.ResponseWriter, r *http.Request) {
    ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
    defer cancel()
    if err := a.store.Ping(ctx); err != nil {
        writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not ready"})
        return
    }
    writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (a *App) shorten(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
        return
    }

    var req shortenRequest
    dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
    if err := dec.Decode(&req); err != nil {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
        return
    }

    req.URL = strings.TrimSpace(req.URL)
    if !isHTTPURL(req.URL) {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url must start with http:// or https://"})
        return
    }
    if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expires_at must be in the future"})
        return
    }

    code := strings.TrimSpace(req.CustomAlias)
    if code != "" {
        if len(code) < 3 || len(code) > 32 || !aliasPattern.MatchString(code) {
            writeJSON(w, http.StatusBadRequest, map[string]string{"error": "custom_alias must be 3-32 characters using A-Z, a-z, 0-9, _ or -"})
            return
        }
    }

    id, err := a.store.CreateURL(r.Context(), req.URL, req.ExpiresAt, code)
    if err != nil {
        if code != "" && strings.Contains(strings.ToLower(err.Error()), "duplicate") {
            writeJSON(w, http.StatusConflict, map[string]string{"error": "alias already exists"})
            return
        }
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save URL"})
        log.Println("create URL:", err)
        return
    }

    if code == "" {
        code = base62.Encode(id)
        if err := a.store.SetCode(r.Context(), id, code); err != nil {
            writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create short code"})
            log.Println("set code:", err)
            return
        }
    }

    writeJSON(w, http.StatusCreated, shortenResponse{
        ShortCode: code,
        ShortURL:  a.baseURL + "/" + code,
        Original:  req.URL,
        ExpiresAt: req.ExpiresAt,
    })
}

func (a *App) redirect(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet && r.Method != http.MethodHead {
        writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET required"})
        return
    }

    code := strings.Trim(r.URL.Path, "/")
    if code == "" || strings.Contains(code, "/") {
        writeJSON(w, http.StatusOK, map[string]string{
            "service": "URL Shortener",
            "message": "POST /shorten to create a short URL",
        })
        return
    }

    ctx := r.Context()
    if original, ok := a.cache.Get(ctx, code); ok {
        a.analytics.Enqueue(analytics.Event{Code: code, IP: clientIP(r), UserAgent: r.UserAgent(), Referer: r.Referer()})
        http.Redirect(w, r, original, http.StatusFound)
        return
    }

    item, err := a.store.GetByCode(ctx, code)
    if err != nil {
        if errors.Is(err, store.ErrNotFound) {
            writeJSON(w, http.StatusNotFound, map[string]string{"error": "short code not found"})
            return
        }
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
        log.Println("lookup:", err)
        return
    }

    now := time.Now()
    if item.DeletedAt != nil || (item.ExpiresAt != nil && !item.ExpiresAt.After(now)) {
        a.cache.Delete(ctx, code)
        writeJSON(w, http.StatusGone, map[string]string{"error": "link expired or deleted"})
        return
    }

    ttl := 10 * time.Minute
    if item.ExpiresAt != nil {
        remaining := time.Until(*item.ExpiresAt)
        if remaining < ttl {
            ttl = remaining
        }
    }
    if ttl > 0 {
        a.cache.SetWithTTL(ctx, code, item.OriginalURL, ttl)
    }

    a.analytics.Enqueue(analytics.Event{Code: code, IP: clientIP(r), UserAgent: r.UserAgent(), Referer: r.Referer()})
    http.Redirect(w, r, item.OriginalURL, http.StatusFound)
}

func (a *App) analyticsHandler(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodGet {
        writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "GET required"})
        return
    }
    code := strings.Trim(r.URL.Path, "/")
    parts := strings.Split(code, "/")
    if len(parts) != 2 || parts[0] != "analytics" || parts[1] == "" {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": "use /analytics/{short_code}"})
        return
    }
    stats, err := a.store.GetAnalytics(r.Context(), parts[1])
    if err != nil {
        if errors.Is(err, store.ErrNotFound) {
            writeJSON(w, http.StatusNotFound, map[string]string{"error": "short code not found"})
            return
        }
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "database error"})
        return
    }
    writeJSON(w, http.StatusOK, stats)
}

func (a *App) deleteURL(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodDelete {
        writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "DELETE required"})
        return
    }
    code := strings.Trim(strings.TrimPrefix(r.URL.Path, "/delete/"), "/")
    if code == "" || strings.Contains(code, "/") {
        writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing short code"})
        return
    }
    if err := a.store.SoftDelete(r.Context(), code); err != nil {
        if errors.Is(err, store.ErrNotFound) {
            writeJSON(w, http.StatusNotFound, map[string]string{"error": "short code not found"})
            return
        }
        writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "delete failed"})
        return
    }
    a.cache.Delete(r.Context(), code)
    writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "short_code": code})
}

func rateLimitMiddleware(a *App, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        key := r.Header.Get("X-API-Key")
        if key == "" {
            key = clientIP(r)
        }
        allowed, err := a.limiter.Allow(r.Context(), key)
        if err != nil {
            writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "rate limiter unavailable"})
            return
        }
        if !allowed {
            w.Header().Set("Retry-After", "60")
            writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
            return
        }
        next.ServeHTTP(w, r)
    })
}

func logging(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        next.ServeHTTP(w, r)
        log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
    })
}

func clientIP(r *http.Request) string {
    if v := r.Header.Get("X-Real-IP"); v != "" {
        return strings.TrimSpace(v)
    }
    if v := r.Header.Get("X-Forwarded-For"); v != "" {
        return strings.TrimSpace(strings.Split(v, ",")[0])
    }
    if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
        return host
    }
    return r.RemoteAddr
}

func isHTTPURL(s string) bool {
    lower := strings.ToLower(s)
    return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    _ = json.NewEncoder(w).Encode(v)
}

func getenv(key, fallback string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return fallback
}

