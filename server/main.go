package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := loadEnvFile(".env"); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("读取 .env 失败: %v", err)
	}

	port := envOrDefault("PORT", "8080")
	cfg := mysqlConfig{
		Host:     envOrDefault("MYSQL_HOST", "127.0.0.1"),
		Port:     envOrDefault("MYSQL_PORT", "3306"),
		User:     envOrDefault("MYSQL_USER", "root"),
		Password: os.Getenv("MYSQL_PASSWORD"),
		Database: envOrDefault("MYSQL_DATABASE", "liuxy_wish"),
	}

	store, err := openStore(cfg)
	if err != nil {
		log.Fatalf("初始化 MySQL 失败: %v", err)
	}
	defer store.Close()

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           newRouter(store),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Printf(
			"许愿服务已启动: http://localhost:%s（MySQL: %s@%s:%s/%s）",
			port,
			cfg.User,
			cfg.Host,
			cfg.Port,
			cfg.Database,
		)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP 服务异常退出: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("服务关闭超时: %v", err)
	}
}
