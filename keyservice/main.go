package main

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Sashreek007/mint/keyservice/internal/api"
	"github.com/Sashreek007/mint/keyservice/internal/cache"
	"github.com/Sashreek007/mint/keyservice/internal/ratelimit"
	"github.com/Sashreek007/mint/keyservice/internal/store"
	"github.com/Sashreek007/mint/keyservice/internal/usage"
)

func main() {
	// Structured JSON logs. SetDefault also routes the stdlib log package through
	// slog, so existing log.Printf calls become JSON too.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))
	// HOSTNAME is set by Docker to the container's short id.
	replicaID := os.Getenv("HOSTNAME")
	if replicaID == "" {
		replicaID = "local"
	}

	// --- config: read required env vars, fail fast if missing ---
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}
	adminToken := os.Getenv("ADMIN_TOKEN")
	if adminToken == "" {
		log.Fatal("ADMIN_TOKEN is required")
	}
	keyPepper := os.Getenv("KEY_PEPPER")
	if keyPepper == "" {
		log.Fatal("KEY_PEPPER is required")
	}
	// KEY_PEPPER_PREV supports pepper rotation: during the rotation window,
	// validation tries the current pepper first, then falls back to the
	// previous one. New keys are always hashed with the current pepper.
	keyPepperPrev := os.Getenv("KEY_PEPPER_PREV")

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		log.Fatal("REDIS_URL is required")
	}
	flushInterval := 30 * time.Second
	if v := os.Getenv("FLUSH_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			flushInterval = d
		}
	}
	prewarmLimit := 1000 // default
	if v := os.Getenv("PREWARM_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			prewarmLimit = n
		}
	}
	rateLimit := 100 // tokens/sec
	if v := os.Getenv("RATE_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			rateLimit = n
		}
	}
	rateBurst := 200 // bucket capacity
	if v := os.Getenv("RATE_BURST"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			rateBurst = n
		}
	}
	// Shared 5s startup deadline for all dependency connects (redis + postgres).
	startupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// --- redis client ---
	redisOpt, err := redis.ParseURL(redisURL)
	if err != nil {
		log.Fatalf("invalid REDIS_URL: %v", err)
	}
	rdb := redis.NewClient(redisOpt)
	defer rdb.Close()

	if err := rdb.Ping(startupCtx).Err(); err != nil {
		log.Fatalf("redis ping failed: %v", err)
	}
	log.Printf("redis ok: %s", redisOpt.Addr)

	// --- connection pool ---
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		log.Fatalf("invalid DATABASE_URL: %v", err)
	}
	cfg.MaxConns = 10
	cfg.MinConns = 2
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(startupCtx, cfg)
	if err != nil {
		log.Fatalf("pgxpool.NewWithConfig: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(startupCtx); err != nil {
		log.Fatalf("postgres ping failed: %v", err)
	}
	log.Printf("postgres ok: max_conns=%d", cfg.MaxConns)

	st := store.New(pool)
	prometheus.MustRegister(store.NewPoolCollector(pool))
	c := cache.New()
	// Pre-warm L1 with the hot key set (skip if PREWARM_LIMIT=0).
	if prewarmLimit > 0 {
		keysList, err := st.RecentActiveKeys(startupCtx, prewarmLimit)
		if err != nil {
			log.Printf("prewarm failed (continuing cold): %v", err)
		} else {
			for _, k := range keysList {
				c.Set(string(k.KeyHash), cache.Result{
					Valid:        true,
					TenantID:     k.TenantID,
					KeyID:        k.KeyID,
					MonthlyQuota: k.MonthlyQuota,
				}, 5*time.Minute) // short TTL; pub/sub + streams evict on revoke
			}
			log.Printf("prewarmed %d keys into L1", len(keysList))
		}
	}
	l2 := cache.NewL2(rdb)
	limiter := ratelimit.New(rateLimit, rateBurst) // 100 req/sec, burst 200, per key

	// Build the pepper list: current pepper first, then previous (if set).
	peppers := []string{keyPepper}
	if keyPepperPrev != "" {
		peppers = append(peppers, keyPepperPrev)
		log.Printf("pepper rotation active: will try current + previous pepper")
	}
	srv := api.New(st, c, l2, rdb, limiter, adminToken, peppers, replicaID)

	go cache.SubscribeRevocations(context.Background(), rdb, c)
	flusherCtx, flusherCancel := context.WithCancel(context.Background())
	flusher := usage.NewFlusher(rdb, st, replicaID, flushInterval)
	go flusher.Run(flusherCtx)

	addr := ":8080"
	httpSrv := &http.Server{
		Addr:    addr,
		Handler: srv.Routes(),
	}

	// Graceful shutdown: drain in-flight requests on SIGTERM/SIGINT.
	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		log.Printf("keyservice replica=%s listening on %s", replicaID, addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server failed: %v", err)
		}
	}()

	sig := <-shutdownCh
	log.Printf("received %s, draining connections…", sig)

	drainCtx, drainCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer drainCancel()

	if err := httpSrv.Shutdown(drainCtx); err != nil {
		log.Printf("http shutdown error: %v", err)
	}

	// Stop the flusher and perform a final flush.
	flusherCancel()
	log.Printf("performing final usage flush…")
	if n, err := flusher.FlushOnce(context.Background()); err != nil {
		log.Printf("final flush failed: %v", err)
	} else if n > 0 {
		log.Printf("final flush: mirrored %d counter(s)", n)
	}

	log.Printf("shutdown complete")
}
