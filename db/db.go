package db

import (
	"context"
	"fmt"
	"log"

	"flower-bot/config"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Database struct {
	pool *pgxpool.Pool
}

// NewDatabase создает новое подключение к БД
func NewDatabase(cfg *config.Config) (*Database, error) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания пула подключений: %w", err)
	}

	// Проверяем подключение
	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ошибка подключения к БД: %w", err)
	}

	log.Println("✅ Подключение к БД успешно")

	db := &Database{pool: pool}

	// Запускаем миграции с конфигом для инициализации настроек
	if err := db.RunMigrations(ctx, cfg); err != nil {
		return nil, fmt.Errorf("ошибка миграций: %w", err)
	}

	return db, nil
}

// GetPool возвращает пул подключений
func (d *Database) GetPool() *pgxpool.Pool {
	return d.pool
}

// Close закрывает подключение к БД
func (d *Database) Close() {
	if d.pool != nil {
		d.pool.Close()
		log.Println("✅ Подключение к БД закрыто")
	}
}

// Query выполняет SELECT запрос
func (d *Database) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	return d.pool.Query(ctx, sql, args...)
}

// QueryRow выполняет SELECT запрос с одной строкой
func (d *Database) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	return d.pool.QueryRow(ctx, sql, args...)
}

// Exec выполняет INSERT/UPDATE/DELETE запрос
func (d *Database) Exec(ctx context.Context, sql string, args ...interface{}) (interface{}, error) {
	return d.pool.Exec(ctx, sql, args...)
}

// BeginTx начинает транзакцию
func (d *Database) BeginTx(ctx context.Context) (pgx.Tx, error) {
	return d.pool.Begin(ctx)
}

// GetShopSettings получает настройки магазина из БД
func (d *Database) GetShopSettings(ctx context.Context) (map[string]interface{}, error) {
	row := d.pool.QueryRow(ctx, `
		SELECT 
			id, shop_name, address, support_user_id, kaspi_link, about_channel_link
		FROM shop_settings LIMIT 1
	`)

	var id int
	var shopName, address, kaspiLink, aboutChannelLink string
	var supportUserID *int64

	if err := row.Scan(&id, &shopName, &address, &supportUserID, &kaspiLink, &aboutChannelLink); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"shop_name":          shopName,
		"address":            address,
		"support_user_id":    supportUserID,
		"kaspi_link":         kaspiLink,
		"about_channel_link": aboutChannelLink,
	}, nil
}
