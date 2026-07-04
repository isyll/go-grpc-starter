// Package db opens pgx connection pools to PostgreSQL per role.
package db

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/isyll/go-grpc-starter/pkg/config"
	"github.com/isyll/go-grpc-starter/pkg/logger"
)

type Role string

const (
	RoleApp       Role = "app"
	RoleAdmin     Role = "admin"
	RoleMigration Role = "migration"
)

const searchPath = "public,auth,notifications,audit,events"

// InitPool opens a pgx connection pool for the given role, with the schema
// search_path and statement timeout applied to every connection.
func InitPool(
	cfg *config.DatabaseConfig,
	role Role,
	l *logger.Logger,
) (*pgxpool.Pool, error) {
	creds, err := credentialsFor(cfg, role)
	if err != nil {
		return nil, fmt.Errorf("database role misconfiguration: %w", err)
	}

	stmtTimeout := cfg.StatementTimeoutMs
	if stmtTimeout <= 0 {
		stmtTimeout = 5000
	}

	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		creds.Host, creds.Port, creds.User, creds.Password,
		creds.DBName, creds.SSLMode,
	)

	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse db config: %w", err)
	}
	poolCfg.ConnConfig.RuntimeParams["search_path"] = searchPath
	poolCfg.ConnConfig.RuntimeParams["statement_timeout"] = strconv.Itoa(stmtTimeout)
	applyPoolConfig(poolCfg, poolFor(cfg, role))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	l.Info("database pool ready", "role", string(role), "user", creds.User)
	return pool, nil
}

func credentialsFor(
	cfg *config.DatabaseConfig, role Role,
) (config.DBCredentials, error) {
	switch role {
	case RoleApp:
		if cfg.AppCredentials.User == "" {
			return config.DBCredentials{}, fmt.Errorf(
				"app role credentials not configured: set DB_API_USER and DB_API_PASSWORD",
			)
		}
		return cfg.AppCredentials, nil
	case RoleAdmin:
		if cfg.AdminCredentials.User == "" {
			return config.DBCredentials{}, fmt.Errorf(
				"admin role credentials not configured: set DB_WORKER_USER and DB_WORKER_PASSWORD",
			)
		}
		return cfg.AdminCredentials, nil
	default:
		return cfg.Credentials, nil
	}
}

func poolFor(cfg *config.DatabaseConfig, role Role) config.ConnectionPoolConfig {
	switch role {
	case RoleApp:
		if cfg.AppPool != nil {
			return *cfg.AppPool
		}
	case RoleAdmin:
		if cfg.AdminPool != nil {
			return *cfg.AdminPool
		}
	case RoleMigration:
		if cfg.MigratePool != nil {
			return *cfg.MigratePool
		}
	}
	return cfg.ConnectionPool
}

func applyPoolConfig(poolCfg *pgxpool.Config, pc config.ConnectionPoolConfig) {
	if pc.MaxOpenConnections > 0 {
		poolCfg.MaxConns = int32(pc.MaxOpenConnections)
	}
	if d, err := time.ParseDuration(pc.ConnectionMaxLifetime); err == nil {
		poolCfg.MaxConnLifetime = d
	}
	if d, err := time.ParseDuration(pc.ConnectionMaxIdleTime); err == nil {
		poolCfg.MaxConnIdleTime = d
	}
}
