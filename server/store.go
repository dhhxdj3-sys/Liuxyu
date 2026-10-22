package main

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"net"
	"regexp"
	"time"

	"github.com/go-sql-driver/mysql"
)

//go:embed schema.sql
var schemaSQL string

var databaseNamePattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)

type mysqlConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
}

type wish struct {
	ID        int64     `json:"id"`
	Wisher    string    `json:"wisher"`
	Wish      string    `json:"wish"`
	CreatedAt time.Time `json:"createdAt"`
}

type wishStore struct {
	db *sql.DB
}

func openStore(cfg mysqlConfig) (*wishStore, error) {
	if !databaseNamePattern.MatchString(cfg.Database) {
		return nil, fmt.Errorf("MYSQL_DATABASE 只能包含字母、数字和下划线")
	}

	baseCfg := mysql.NewConfig()
	baseCfg.User = cfg.User
	baseCfg.Passwd = cfg.Password
	baseCfg.Net = "tcp"
	baseCfg.Addr = net.JoinHostPort(cfg.Host, cfg.Port)
	baseCfg.ParseTime = true
	baseCfg.Loc = time.UTC
	baseCfg.Timeout = 5 * time.Second
	baseCfg.ReadTimeout = 10 * time.Second
	baseCfg.WriteTimeout = 10 * time.Second
	baseCfg.Params = map[string]string{"charset": "utf8mb4"}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 先连接 MySQL 实例本身，确保数据库不存在时也能自动创建。
	adminCfg := *baseCfg
	adminCfg.DBName = ""
	adminDB, err := sql.Open("mysql", adminCfg.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("初始化 MySQL 连接: %w", err)
	}
	defer adminDB.Close()
	adminDB.SetMaxOpenConns(1)

	if err := adminDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("连接 MySQL（%s）: %w", adminCfg.Addr, err)
	}

	createDatabaseSQL := fmt.Sprintf(
		"CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci",
		cfg.Database,
	)
	if _, err := adminDB.ExecContext(ctx, createDatabaseSQL); err != nil {
		return nil, fmt.Errorf("创建数据库 %s: %w", cfg.Database, err)
	}

	appCfg := *baseCfg
	appCfg.DBName = cfg.Database
	db, err := sql.Open("mysql", appCfg.FormatDSN())
	if err != nil {
		return nil, fmt.Errorf("连接数据库 %s: %w", cfg.Database, err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(3 * time.Minute)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("连接数据库 %s: %w", cfg.Database, err)
	}

	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化 wishes 表: %w", err)
	}

	return &wishStore{db: db}, nil
}

func (s *wishStore) Close() error {
	return s.db.Close()
}

func (s *wishStore) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *wishStore) Create(ctx context.Context, wisher, wishText string, createdAt time.Time) (wish, error) {
	createdAt = createdAt.UTC().Truncate(time.Microsecond)
	result, err := s.db.ExecContext(
		ctx,
		"INSERT INTO wishes (wisher, wish, created_at) VALUES (?, ?, ?)",
		wisher,
		wishText,
		createdAt,
	)
	if err != nil {
		return wish{}, fmt.Errorf("写入愿望: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return wish{}, fmt.Errorf("读取愿望 ID: %w", err)
	}

	return wish{
		ID:        id,
		Wisher:    wisher,
		Wish:      wishText,
		CreatedAt: createdAt,
	}, nil
}
