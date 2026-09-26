// Command forgejo-github-bridge keeps issues and comments in step between a
// private Forgejo and the public GitHub repositories it push-mirrors to.
//
// It runs inside the private network and only ever connects out: to GitHub's
// API, to Forgejo and to its database. Its one listener serves probes and
// metrics to the cluster and nothing to the internet.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/bridge"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forgejo"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/github"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/gitops"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store/postgres"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/telemetry"
)

var (
	version = "dev"
	commit  = "unknown"
	builtAt = "unknown"
)

// httpTimeout bounds every request to either forge. A paginated list is many
// requests, each bounded on its own.
const httpTimeout = 30 * time.Second

func main() {
	if err := run(); err != nil {
		// slog may not exist yet when configuration itself failed, so this one
		// message goes to stderr directly.
		_, _ = os.Stderr.WriteString("fatal: " + err.Error() + "\n")
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg.LogFormat, cfg.LogLevel)
	slog.SetDefault(log)
	telemetry.SetBuildInfo(version, commit, builtAt)
	keys := make([]string, len(cfg.Repositories))
	for i, r := range cfg.Repositories {
		keys[i] = r.Key()
	}
	telemetry.Register(keys)

	repos := make([]string, len(cfg.Repositories))
	for i, r := range cfg.Repositories {
		repos[i] = r.GitHub.String() + "=" + r.Forgejo.String()
	}
	log.Info("starting forgejo-github-bridge",
		"version", version, "commit", commit, "built_at", builtAt,
		"forgejo", cfg.ForgejoURL.String(), "repositories", repos,
		"poll_interval", cfg.PollInterval, "full_resync", cfg.FullResync)

	if err := bridge.CheckMessages(cfg.Messages, cfg.Repositories); err != nil {
		return fmt.Errorf("messages: %w", err)
	}

	key, err := github.LoadPrivateKey(cfg.GitHubPrivateKeyFile)
	if err != nil {
		return err
	}
	forgejoHTTP, err := forgejoClient(cfg.ForgejoCAFile)
	if err != nil {
		return err
	}

	// The startup context is bounded. A database that never answers should
	// fail the pod rather than leave it starting forever with no explanation.
	startCtx, cancelStart := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancelStart()
	st, err := postgres.Open(startCtx, cfg.DatabaseURL, cfg.DatabaseMaxConns, log)
	if err != nil {
		return err
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errs := make(chan error, 1)
	srv := serve(cfg.ListenAddr, st, errs, log)
	defer shutdown(srv, log)

	// A second replica waits here until the first one's session ends
	// (docs/design.md §7). It stays ready meanwhile: readiness is about whether
	// it could take over, not about whether it is doing the work.
	lease, err := postgres.AcquireLeader(ctx, st.Pool(), log)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	telemetry.SetLeader(true)
	log.Info("holding the leader lock; this instance does the work")

	gh := github.New(cfg.GitHubAPIURL, cfg.GitHubAppID, key, &http.Client{Timeout: httpTimeout}, telemetry.ObserveGitHub)
	fj := forgejo.New(cfg.ForgejoURL, cfg.ForgejoToken, forgejoHTTP)
	b := bridge.New(st, gh, fj, cfg.Repositories, bridge.Options{
		PollInterval: cfg.PollInterval,
		FullResync:   cfg.FullResync,
		ForgejoURLs:  cfg.ForgejoURLs(),
		UserMapping:  cfg.UserMapping,
		Messages:     cfg.Messages,
	}, log)
	if cfg.ForgejoContribToken != "" {
		contrib := forgejo.New(cfg.ForgejoURL, cfg.ForgejoContribToken, forgejoHTTP)
		login, err := contrib.Self(ctx)
		if err != nil {
			return fmt.Errorf("identifying the Forgejo contrib account: %w", err)
		}
		git := gitops.New(cfg.GitCacheDir, "https://github.com", cfg.ForgejoURL, login, cfg.ForgejoContribToken, cfg.ForgejoCAFile).
			WithGitHubToken(gh.InstallationToken)
		b.WithPullRequests(bridge.Pulls{GitHub: gh, Forgejo: fj, Contrib: contrib, Git: git})
	}
	if err := b.Init(ctx); err != nil {
		return err
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	go func() {
		select {
		case <-lease.Lost():
			cancelRun()
		case <-runCtx.Done():
		}
	}()
	go func() {
		if err := b.Run(runCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("the bridge stopped", "error", err)
		}
		cancelRun()
	}()

	select {
	case err := <-errs:
		return err
	case <-runCtx.Done():
	}
	if ctx.Err() != nil {
		log.Info("shutting down")
		return nil
	}
	// Losing the lock means another instance may already be active. Exiting
	// is the only way to be sure this one is not doing the same work.
	return errors.New("lost the leader lock")
}

// forgejoClient trusts the system roots plus, when configured, the private
// CA Forgejo's certificate is issued by.
func forgejoClient(caFile string) (*http.Client, error) {
	if caFile == "" {
		return &http.Client{Timeout: httpTimeout}, nil
	}
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("reading the Forgejo CA: %w", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("%s holds no PEM certificate", caFile)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return &http.Client{Timeout: httpTimeout, Transport: transport}, nil
}

func serve(addr string, st *postgres.Store, errs chan<- error, log *slog.Logger) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := st.Ping(ctx); err != nil {
			http.Error(w, "database: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("GET /metrics", promhttp.Handler())

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Info("listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- fmt.Errorf("listener on %s: %w", addr, err)
		}
	}()
	return srv
}

func shutdown(srv *http.Server, log *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Warn("shutting the listener down", "error", err)
	}
}

func newLogger(format, level string) *slog.Logger {
	var lvl slog.Level
	_ = lvl.UnmarshalText([]byte(level))
	opts := &slog.HandlerOptions{Level: lvl}
	if format == "text" {
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}
