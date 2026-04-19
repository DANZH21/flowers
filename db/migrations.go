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
		// Настройки салона
		`CREATE TABLE IF NOT EXISTS salon_settings (
			id SERIAL PRIMARY KEY,
			salon_name TEXT NOT NULL DEFAULT '',
			address TEXT NOT NULL DEFAULT '',
			support_user_id BIGINT,
			kaspi_link TEXT NOT NULL DEFAULT '',
			about_channel_link TEXT NOT NULL DEFAULT '',
			schedule_open TEXT NOT NULL DEFAULT '10:00',
			schedule_close TEXT NOT NULL DEFAULT '20:00',
			reminder_hours INT NOT NULL DEFAULT 1,
			prepay_percent INT NOT NULL DEFAULT 0,
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

		// Услуги в салоне
		`CREATE TABLE IF NOT EXISTS services (
			id SERIAL PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			price NUMERIC(10,2) NOT NULL,
			duration_min INT NOT NULL DEFAULT 30,
			is_available BOOLEAN DEFAULT TRUE,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Записи на услуги
		`CREATE TABLE IF NOT EXISTS appointments (
			id SERIAL PRIMARY KEY,
			appointment_num SERIAL,
			user_id BIGINT NOT NULL REFERENCES users(telegram_id),
			service_id INT NOT NULL REFERENCES services(id),
			appointment_time TIMESTAMPTZ NOT NULL,
			payment_type TEXT NOT NULL,
			amount NUMERIC(10,2) NOT NULL,
			prepay_amount NUMERIC(10,2),
			customer_name TEXT,
			customer_phone TEXT,
			status TEXT NOT NULL DEFAULT 'scheduled',
			receipt_url TEXT,
			receipt_status TEXT DEFAULT 'pending',
			receipt_deadline TIMESTAMPTZ,
			reminder_sent BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// Индексы для оптимизации запросов
		`CREATE INDEX IF NOT EXISTS idx_appointments_user_id ON appointments(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_appointments_status ON appointments(status)`,
		`CREATE INDEX IF NOT EXISTS idx_appointments_time ON appointments(appointment_time)`,
		`CREATE INDEX IF NOT EXISTS idx_appointments_service_id ON appointments(service_id)`,
		`CREATE INDEX IF NOT EXISTS idx_services_available ON services(is_available)`,
		`CREATE INDEX IF NOT EXISTS idx_users_is_admin ON users(is_admin)`,

		// Добавляем колонку receipt_status если её нет
		`ALTER TABLE appointments ADD COLUMN IF NOT EXISTS receipt_status TEXT DEFAULT 'pending'`,

		// Защита записей (наличные только после N заказов)
		`ALTER TABLE salon_settings ADD COLUMN IF NOT EXISTS protection_enabled BOOLEAN DEFAULT FALSE`,
		`ALTER TABLE salon_settings ADD COLUMN IF NOT EXISTS protection_min_orders INT DEFAULT 0`,

		// Один мастер (запрет параллельных записей)
		`ALTER TABLE salon_settings ADD COLUMN IF NOT EXISTS is_one_master BOOLEAN DEFAULT FALSE`,
	}

	for _, migration := range migrations {
		if _, err := d.pool.Exec(ctx, migration); err != nil {
			log.Printf("❌ Ошибка миграции: %v\n", err)
			return err
		}
	}

	log.Println("✅ Миграции выполнены успешно")

	// Инициализируем salon_settings если они не существуют
	row := d.pool.QueryRow(ctx, "SELECT COUNT(*) FROM salon_settings")
	var count int
	if err := row.Scan(&count); err != nil {
		return err
	}

	if count == 0 {
		salonName := cfg.ShopName
		if salonName == "" {
			salonName = "Beauty Salon"
		}
		salonAddress := cfg.ShopAddress
		if salonAddress == "" {
			salonAddress = "Almaty, Kazakhstan"
		}
		_, err := d.pool.Exec(ctx,
			`INSERT INTO salon_settings (salon_name, address, support_user_id, kaspi_link, about_channel_link, schedule_open, schedule_close, reminder_hours, prepay_percent)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			salonName, salonAddress, cfg.SupportUserID, cfg.KaspiLink, cfg.AboutChannelLink, "10:00", "20:00", 1, 0)
		if err != nil {
			log.Printf("❌ Ошибка инициализации salon_settings: %v\n", err)
			return err
		}
		log.Println("✅ salon_settings инициализированы")
	}

	// Инициализируем услуги по умолчанию если их нет
	serviceRow := d.pool.QueryRow(ctx, "SELECT COUNT(*) FROM services")
	var serviceCount int
	if err := serviceRow.Scan(&serviceCount); err != nil {
		return err
	}

	if serviceCount == 0 {
		defaultServices := []struct {
			name     string
			price    float64
			duration int
		}{
			{"Маникюр", 8000, 60},
			{"Педикюр", 10000, 90},
			{"Покрытие ногтей", 5000, 30},
			{"Наращивание ногтей", 12000, 120},
			{"Дизайн ногтей", 3000, 20},
			{"Шеллак", 6000, 45},
		}

		for _, svc := range defaultServices {
			_, err := d.pool.Exec(ctx,
				`INSERT INTO services (name, price, duration_min, is_available) VALUES ($1, $2, $3, TRUE)`,
				svc.name, svc.price, svc.duration)
			if err != nil {
				log.Printf("⚠️ Ошибка добавления услуги %s: %v\n", svc.name, err)
			}
		}
		log.Println("✅ Дефолтные услуги добавлены (6 шт)")
	}

	return nil
}
