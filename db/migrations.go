package db

import (
	"context"
	"log"

	"flower-bot/config"
)

// RunMigrations запускает все миграции при старте
func (d *Database) RunMigrations(ctx context.Context, cfg *config.Config) error {
	log.Println("🔧 Запуск миграций...")

	migrations := []string{
		// Настройки магазина
		`CREATE TABLE IF NOT EXISTS shop_settings (
			id SERIAL PRIMARY KEY,
			shop_name TEXT NOT NULL DEFAULT '',
			address TEXT NOT NULL DEFAULT '',
			support_user_id BIGINT,
			kaspi_link TEXT NOT NULL DEFAULT '',
			about_channel_link TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Пользователи
		`CREATE TABLE IF NOT EXISTS users (
			telegram_id BIGINT PRIMARY KEY,
			username TEXT,
			full_name TEXT,
			phone TEXT,
			is_admin BOOLEAN DEFAULT FALSE,
			last_order_attempt TIMESTAMPTZ,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Букеты каталога
		`CREATE TABLE IF NOT EXISTS bouquets (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			price NUMERIC(10,2) NOT NULL,
			photo_url TEXT NOT NULL,
			quantity INT NOT NULL DEFAULT 1,
			is_available BOOLEAN DEFAULT TRUE,
			reserved_until TIMESTAMPTZ,
			reserved_by BIGINT,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Заказы
		`CREATE TABLE IF NOT EXISTS orders (
			id SERIAL PRIMARY KEY,
			order_number SERIAL,
			user_id BIGINT NOT NULL REFERENCES users(telegram_id),
			bouquet_id INT REFERENCES bouquets(id),
			custom_order_id INT,
			delivery_type TEXT NOT NULL,
			payment_type TEXT NOT NULL,
			amount NUMERIC(10,2) NOT NULL,
			prepay_amount NUMERIC(10,2),
			customer_name TEXT,
			customer_phone TEXT,
			delivery_address TEXT,
			status TEXT NOT NULL DEFAULT 'pending',
			receipt_url TEXT,
			receipt_deadline TIMESTAMPTZ,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Кастомные букеты
		`CREATE TABLE IF NOT EXISTS custom_orders (
			id SERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL REFERENCES users(telegram_id),
			description TEXT NOT NULL,
			admin_price NUMERIC(10,2),
			status TEXT DEFAULT 'pending',
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Индексы для оптимизации запросов
		`CREATE INDEX IF NOT EXISTS idx_orders_user_id ON orders(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_orders_status ON orders(status)`,
		`CREATE INDEX IF NOT EXISTS idx_orders_created_at ON orders(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_custom_orders_user_id ON custom_orders(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_bouquets_available ON bouquets(is_available)`,
		`CREATE INDEX IF NOT EXISTS idx_users_is_admin ON users(is_admin)`,
	}

	for _, migration := range migrations {
		if _, err := d.pool.Exec(ctx, migration); err != nil {
			log.Printf("❌ Ошибка миграции: %v\n", err)
			return err
		}
	}

	log.Println("✅ Миграции выполнены успешно")

	// Инициализируем shop_settings если они не существуют
	row := d.pool.QueryRow(ctx, "SELECT COUNT(*) FROM shop_settings")
	var count int
	if err := row.Scan(&count); err != nil {
		return err
	}

	if count == 0 {
		shopName := cfg.ShopName
		if shopName == "" {
			shopName = "Flower Shop"
		}
		shopAddress := cfg.ShopAddress
		if shopAddress == "" {
			shopAddress = "Almaty, Kazakhstan"
		}
		_, err := d.pool.Exec(ctx,
			`INSERT INTO shop_settings (shop_name, address, support_user_id, kaspi_link, about_channel_link)
			VALUES ($1, $2, $3, $4, $5)`,
			shopName, shopAddress, cfg.SupportUserID, cfg.KaspiLink, cfg.AboutChannelLink)
		if err != nil {
			log.Printf("❌ Ошибка инициализации shop_settings: %v\n", err)
			return err
		}
		log.Println("✅ shop_settings инициализированы")
	}

	return nil
}
