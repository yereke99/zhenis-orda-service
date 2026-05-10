package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"zhenis-orda-service/config"
	"zhenis-orda-service/internal/handler"
	"zhenis-orda-service/internal/repository"
	"zhenis-orda-service/traits/database"
	"zhenis-orda-service/traits/logger"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	log, err := logger.New(cfg.Env)
	if err != nil {
		panic(err)
	}
	defer log.Sync()
	log.Info("starting ZHENIS ORDA INSIDE", zap.String("config", cfg.String()))

	db, err := database.Open(ctx, cfg.DBPath)
	if err != nil {
		log.Fatal("database open failed", zap.Error(err))
	}
	defer db.Close()
	if err := database.Migrate(ctx, db); err != nil {
		log.Fatal("database migrate failed", zap.Error(err))
	}

	store := repository.New(db)
	kv := buildKV(ctx, cfg, log)

	srv := handler.NewServer(cfg, store, kv, log)
	var bot *handler.TelegramBot
	if !cfg.DisableTelegramBot {
		bot = handler.NewTelegramBot(cfg.Token, store, kv, cfg.UploadDir, cfg.MiniAppURL, cfg.AdminIDs, cfg.MaxReceiptBytes, log)
		srv.SetBot(bot)
		bot.StartLongPolling(ctx)
	}
	srv.StartSchedulers(ctx)

	httpServer := &http.Server{
		Addr:              cfg.Addr(),
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Info("http server listening", zap.String("addr", cfg.Addr()))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("http server failed", zap.Error(err))
		}
	}()

	<-ctx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error("http shutdown failed", zap.Error(err))
	}
	log.Info("shutdown complete")
}

func buildKV(ctx context.Context, cfg config.Config, log *zap.Logger) handler.KV {
	if cfg.RedisAddr == "" {
		log.Warn("REDIS_ADDR is empty; using in-memory sessions and states")
		return handler.NewMemoryKV()
	}
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	if err := client.Ping(ctx).Err(); err != nil {
		log.Warn("redis unavailable; using in-memory sessions and states", zap.Error(err))
		_ = client.Close()
		return handler.NewMemoryKV()
	}
	log.Info("redis connected", zap.String("addr", cfg.RedisAddr))
	return handler.NewRedisKV(client)
}
