package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"flower-bot/config"
	"flower-bot/db"
	"flower-bot/handlers"
	"flower-bot/models"
	"flower-bot/scheduler"
	"flower-bot/services"

	"gopkg.in/telebot.v3"
)

func main() {
	// Загружаем конфиг
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("❌ Ошибка загрузки конфига: %v\n", err)
	}

	// Выводим конфигурацию
	log.Printf("📋 [КОНФИГ] Loaded Admin IDs: %v (количество: %d)\n", cfg.AdminIDs, len(cfg.AdminIDs))
	if len(cfg.AdminIDs) == 0 {
		log.Println("⚠️ [КОНФИГ] ВНИМАНИЕ: ADMIN_IDS не установлен или пуст!")
		log.Println("⚠️ [КОНФИГ] Установите переменную ADMIN_IDS в формате: ADMIN_IDS=123,456,789")
	}

	// Проверяем обязательные переменные
	if cfg.TelegramBotToken == "" {
		log.Fatalf("❌ TELEGRAM_BOT_TOKEN не установлен")
	}

	if cfg.DatabaseURL == "" {
		log.Fatalf("❌ DATABASE_URL не установлен")
	}

	// Инициализация S3
	if err := services.InitS3(cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket, cfg.S3Region, cfg.S3Endpoint, cfg.S3PublicURL); err != nil {
		log.Printf("⚠️ S3 not initialized properly or credentials missing: %v", err)
	}

	log.Println("🚀 Запуск Beauty Salon Bot...")
	log.Println("💅 ===== BEAUTY SALON BOT STARTING =====")

	// Подключаемся к БД
	log.Println("🔄 [ИНИЦИАЛИЗАЦИЯ] Подключение к БД...")
	database, err := db.NewDatabase(cfg)
	if err != nil {
		log.Fatalf("❌ [ОШИБКА] Ошибка подключения к БД: %v\n", err)
	}
	defer database.Close()
	log.Println("✅ [БД] Подключение установлено и миграции выполнены")

	// Создаем бота
	log.Println("🤖 [ИНИЦИАЛИЗАЦИЯ] Инициализация Telegram бота...")
	bot, err := telebot.NewBot(telebot.Settings{
		Token:  cfg.TelegramBotToken,
		Poller: &telebot.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		log.Fatalf("❌ [ОШИБКА] Ошибка создания бота: %v\n", err)
	}

	log.Printf("✅ [БОТ] Бот инициализирован: @%s\n", bot.Me.Username)

	// Создаем менеджер состояний
	log.Println("📊 [ИНИЦИАЛИЗАЦИЯ] Создание менеджера состояний...")
	stateManager := handlers.NewStateManager()
	log.Println("✅ [СОСТОЯНИЯ] Менеджер состояний создан")

	// Создаем планировщик
	log.Println("⏱️ [ИНИЦИАЛИЗАЦИЯ] Создание планировщика задач...")
	sch := scheduler.NewScheduler(database, bot)
	log.Println("✅ [ПЛАНИРОВЩИК] Планировщик создан")

	// Создаем обработчики клиента и админа
	log.Println("🎯 [ИНИЦИАЛИЗАЦИЯ] Создание обработчиков...")
	clientHandler := handlers.NewClientHandler(database, bot, stateManager, sch, cfg.AdminIDs)
	adminHandler := handlers.NewAdminHandler(database, bot, stateManager, cfg.AdminIDs)
	log.Println("✅ [ОБРАБОТЧИКИ] Обработчики созданы")

	// ========== КОМАНДЫ ==========
	log.Println("📝 [РЕГИСТРАЦИЯ] Регистрирую команды...")

	// /start
	bot.Handle("/start", func(c telebot.Context) error {
		log.Printf("🔵 [COMMAND] /start от пользователя %d\n", c.Sender().ID)
		return clientHandler.HandleStart(c)
	})

	// /admin
	bot.Handle("/admin", func(c telebot.Context) error {
		userID := c.Sender().ID
		log.Printf("🔴 [COMMAND] /admin от пользователя %d\n", userID)
		log.Printf("   Проверяю права администратора...\n")
		isAdmin := adminHandler.IsAdmin(userID)
		log.Printf("   IsAdmin: %v\n", isAdmin)
		if !isAdmin {
			log.Printf("   ❌ Пользователь %d не является администратором\n", userID)
			return c.Send("❌ Доступ запрещён. Вы не администратор.\n\nАдминистратор ID: " + fmt.Sprintf("%d", userID))
		}
		log.Printf("   ✅ Пользователь %d имеет права администратора\n", userID)
		return adminHandler.HandleAdminMenu(c)
	})

	// ========== INLINE КНОПКИ ==========

	// Главное меню
	bot.Handle(&telebot.Btn{Unique: "main_menu"}, func(c telebot.Context) error {
		log.Printf("🏠 [BUTTON] Пользователь %d нажал 'Меню'\n", c.Sender().ID)
		c.Respond()
		return clientHandler.HandleStart(c)
	})

	// Клиент - запись на услугу
	bot.Handle(&telebot.Btn{Unique: "book_appointment"}, func(c telebot.Context) error {
		log.Printf("📅 [BUTTON] Пользователь %d нажал 'Записаться'\n", c.Sender().ID)
		c.Respond()
		return clientHandler.HandleBookAppointment(c)
	})

	// Клиент - мои записи
	bot.Handle(&telebot.Btn{Unique: "my_appointments"}, func(c telebot.Context) error {
		log.Printf("📋 [BUTTON] Пользователь %d нажал 'Мои записи'\n", c.Sender().ID)
		c.Respond()
		return clientHandler.HandleMyAppointments(c)
	})

	// Выбор услуги
	bot.Handle(telebot.OnCallback, func(c telebot.Context) error {
		data := c.Callback().Data
		userID := c.Sender().ID

		log.Printf("🔘 [CALLBACK] Пользователь %d нажал кнопку: %s\n", userID, data)

		if len(data) > 15 && data[:15] == "select_service_" {
			var serviceID int
			_, err := sscanf(data, "select_service_%d", &serviceID)
			if err == nil {
				log.Printf("✅ [CALLBACK] Маршрутизирую на HandleSelectService(%d)\n", serviceID)
				c.Respond()
				return clientHandler.HandleSelectService(c, serviceID)
			}
			log.Printf("❌ [CALLBACK] Ошибка парсинга select_service: %v\n", err)
		}

		if len(data) > 12 && data[:12] == "select_date_" {
			dateStr := data[12:]
			log.Printf("✅ [CALLBACK] Маршрутизирую на HandleSelectDate(%s)\n", dateStr)
			c.Respond()
			return clientHandler.HandleSelectDate(c, dateStr)
		}

		if len(data) > 12 && data[:12] == "select_time_" {
			// Формат: select_time_2024-01-15_14:30
			parts := parseDateTimeCallback(data[12:])
			if len(parts) == 2 {
				log.Printf("✅ [CALLBACK] Маршрутизирую на HandleSelectTime(%s, %s)\n", parts[0], parts[1])
				c.Respond()
				return clientHandler.HandleSelectTime(c, parts[0], parts[1])
			}
			log.Printf("❌ [CALLBACK] Ошибка парсинга select_time: %v\n", data)
		}

		if len(data) > 15 && data[:15] == "payment_kaspi_" {
			log.Printf("✅ [CALLBACK] Маршрутизирую на HandlePaymentKaspi\n")
			c.Respond()
			return clientHandler.HandlePaymentKaspi(c, userID)
		}

		if len(data) > 13 && data[:13] == "payment_cash_" {
			log.Printf("✅ [CALLBACK] Маршрутизирую на HandlePaymentCash\n")
			c.Respond()
			return clientHandler.HandlePaymentCash(c, userID)
		}

		// Админ панель
		if data == "admin_appointments" {
			log.Printf("✅ [CALLBACK] Админ %d -> HandleAdminAppointments\n", userID)
			if !adminHandler.IsAdmin(userID) {
				log.Printf("❌ [CALLBACK] Пользователь %d не администратор!\n", userID)
				c.Respond()
				return c.Send("❌ Доступ запрещён")
			}
			c.Respond()
			err := adminHandler.HandleAdminAppointments(c)
			if err != nil {
				log.Printf("❌ [ERROR] HandleAdminAppointments ошибка: %v\n", err)
			}
			return err
		}

		if data == "admin_services" {
			log.Printf("✅ [CALLBACK] Админ %d -> HandleAdminServices\n", userID)
			if !adminHandler.IsAdmin(userID) {
				log.Printf("❌ [CALLBACK] Пользователь %d не администратор!\n", userID)
				c.Respond()
				return c.Send("❌ Доступ запрещён")
			}
			c.Respond()
			err := adminHandler.HandleAdminServices(c)
			if err != nil {
				log.Printf("❌ [ERROR] HandleAdminServices ошибка: %v\n", err)
			}
			return err
		}

		if data == "admin_settings" {
			log.Printf("✅ [CALLBACK] Админ %d -> HandleAdminSettings\n", userID)
			if !adminHandler.IsAdmin(userID) {
				log.Printf("❌ [CALLBACK] Пользователь %d не администратор!\n", userID)
				c.Respond()
				return c.Send("❌ Доступ запрещён")
			}
			c.Respond()
			err := adminHandler.HandleAdminSettings(c)
			if err != nil {
				log.Printf("❌ [ERROR] HandleAdminSettings ошибка: %v\n", err)
			}
			return err
		}

		if data == "admin_add_service" {
			log.Printf("✅ [CALLBACK] Админ %d -> HandleAdminAddService\n", userID)
			if !adminHandler.IsAdmin(userID) {
				log.Printf("❌ [CALLBACK] Пользователь %d не администратор!\n", userID)
				c.Respond()
				return c.Send("❌ Доступ запрещён")
			}
			c.Respond()
			return adminHandler.HandleAdminAddService(c)
		}

		if data == "admin_set_name" {
			log.Printf("✅ [CALLBACK] Админ %d -> HandleAdminSetName\n", userID)
			if !adminHandler.IsAdmin(userID) {
				log.Printf("❌ [CALLBACK] Пользователь %d не администратор!\n", userID)
				c.Respond()
				return c.Send("❌ Доступ запрещён")
			}
			c.Respond()
			return adminHandler.HandleAdminSetName(c)
		}

		if data == "admin_set_address" {
			log.Printf("✅ [CALLBACK] Админ %d -> HandleAdminSetAddress\n", userID)
			if !adminHandler.IsAdmin(userID) {
				log.Printf("❌ [CALLBACK] Пользователь %d не администратор!\n", userID)
				c.Respond()
				return c.Send("❌ Доступ запрещён")
			}
			c.Respond()
			return adminHandler.HandleAdminSetAddress(c)
		}

		if data == "admin_set_open" {
			log.Printf("✅ [CALLBACK] Админ %d -> HandleAdminSetScheduleOpen\n", userID)
			if !adminHandler.IsAdmin(userID) {
				log.Printf("❌ [CALLBACK] Пользователь %d не администратор!\n", userID)
				c.Respond()
				return c.Send("❌ Доступ запрещён")
			}
			c.Respond()
			return adminHandler.HandleAdminSetScheduleOpen(c)
		}

		if data == "admin_set_close" {
			log.Printf("✅ [CALLBACK] Админ %d -> HandleAdminSetScheduleClose\n", userID)
			if !adminHandler.IsAdmin(userID) {
				log.Printf("❌ [CALLBACK] Пользователь %d не администратор!\n", userID)
				c.Respond()
				return c.Send("❌ Доступ запрещён")
			}
			c.Respond()
			return adminHandler.HandleAdminSetScheduleClose(c)
		}

		if data == "admin_set_reminder" {
			log.Printf("✅ [CALLBACK] Админ %d -> HandleAdminSetReminderHours\n", userID)
			if !adminHandler.IsAdmin(userID) {
				log.Printf("❌ [CALLBACK] Пользователь %d не администратор!\n", userID)
				c.Respond()
				return c.Send("❌ Доступ запрещён")
			}
			c.Respond()
			return adminHandler.HandleAdminSetReminderHours(c)
		}

		if data == "admin_set_prepay" {
			log.Printf("✅ [CALLBACK] Админ %d -> HandleAdminSetPrepayPercent\n", userID)
			if !adminHandler.IsAdmin(userID) {
				log.Printf("❌ [CALLBACK] Пользователь %d не администратор!\n", userID)
				c.Respond()
				return c.Send("❌ Доступ запрещён")
			}
			c.Respond()
			return adminHandler.HandleAdminSetPrepayPercent(c)
		}

		if data == "admin_set_support" {
			log.Printf("✅ [CALLBACK] Админ %d -> HandleAdminSetSupportID\n", userID)
			if !adminHandler.IsAdmin(userID) {
				log.Printf("❌ [CALLBACK] Пользователь %d не администратор!\n", userID)
				c.Respond()
				return c.Send("❌ Доступ запрещён")
			}
			c.Respond()
			return adminHandler.HandleAdminSetSupportID(c)
		}

		// Подтверждение чека администратором
		if len(data) > 15 && data[:15] == "confirm_receipt_" {
			var appointmentID int
			_, err := fmt.Sscanf(data, "confirm_receipt_%d", &appointmentID)
			if err == nil {
				log.Printf("✅ [CALLBACK] Админ %d -> HandleConfirmReceipt(%d)\n", userID, appointmentID)
				c.Respond()
				return adminHandler.HandleConfirmReceipt(c, appointmentID)
			}
		}

		// Отклонение чека администратором
		if len(data) > 14 && data[:14] == "reject_receipt_" {
			var appointmentID int
			_, err := fmt.Sscanf(data, "reject_receipt_%d", &appointmentID)
			if err == nil {
				log.Printf("✅ [CALLBACK] Админ %d -> HandleRejectReceipt(%d)\n", userID, appointmentID)
				c.Respond()
				return adminHandler.HandleRejectReceipt(c, appointmentID)
			}
		}

		// Кнопки главного меню
		if data == "about" {
			log.Printf("📋 [CALLBACK] Пользователь %d запросил информацию о салоне\n", userID)
			c.Respond()
			return c.Send("ℹ️ *BEAUTY SALON*\n\nМы предоставляем профессиональные услуги маникюра и педикюра 💅\n\n📍 Адрес: Алматы\n⏰ Режим работы: 10:00 - 20:00\n📞 Контакт: +7 700 000 00 00")
		}

		if data == "support" {
			log.Printf("💬 [CALLBACK] Пользователь %d запросил поддержку\n", userID)
			c.Respond()
			return c.Send("📞 *Служба поддержки*\n\nЕсли у вас есть вопросы, пожалуйста свяжитесь с нами:\n\n📧 Email: support@salon.kz\n💬 Telegram: @salon_support\n☎️ WhatsApp: +7 700 000 00 00")
		}

		log.Printf("⚠️ [CALLBACK] Неизвестная кнопка от пользователя %d: %s\n", userID, data)
		c.Respond()
		return c.Edit("❌ Неизвестная кнопка")
	})

	// ========== ТЕКСТОВЫЕ СООБЩЕНИЯ И ФОТ ==========

	// Обработчик для текстовых сообщений с FSM
	bot.Handle(telebot.OnText, func(c telebot.Context) error {
		userID := c.Sender().ID
		state := stateManager.GetState(userID)
		text := c.Message().Text

		log.Printf("💬 [TEXT] Пользователь %d отправил текст (состояние %v): %s\n", userID, state, text[:min(len(text), 50)])

		// FSM обработчики
		switch state {
		case models.StateAwaitingName:
			return clientHandler.HandleNameInput(c)
		case models.StateAwaitingPhone:
			return clientHandler.HandlePhoneInput(c)
		case models.StateAdminSetSalonName:
			return adminHandler.HandleAdminInputSalonName(c)
		case models.StateAdminSetAddress:
			return adminHandler.HandleAdminInputAddress(c)
		case models.StateAdminSetScheduleOpen:
			return adminHandler.HandleAdminInputScheduleOpen(c)
		case models.StateAdminSetScheduleClose:
			return adminHandler.HandleAdminInputScheduleClose(c)
		case models.StateAdminSetReminderHours:
			return adminHandler.HandleAdminInputReminderHours(c)
		case models.StateAdminSetPrepayPercent:
			return adminHandler.HandleAdminInputPrepayPercent(c)
		case models.StateAdminSetSupportID:
			return adminHandler.HandleAdminInputSupportID(c)
		}

		return c.Send("❓ Я не знаю как ответить на это. Нажмите /start для главного меню.")
	})

	// Обработчик для загрузки фото (чек)
	bot.Handle(telebot.OnPhoto, func(c telebot.Context) error {
		userID := c.Sender().ID
		state := stateManager.GetState(userID)

		log.Printf("📷 [PHOTO] Пользователь %d отправил фото (состояние %v)\n", userID, state)

		if state == models.StateAwaitingReceipt {
			return clientHandler.HandleReceiptUpload(c)
		}

		return c.Send("❌ В данный момент фото не требуются")
	})

	// ========== GRACEFUL SHUTDOWN ==========

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Запускаем бота в горутине
	go func() {
		log.Println("🔄 Бот запущен и слушает сообщения...")
		bot.Start()
	}()

	// Ждем сигнала завершения
	<-sigChan
	log.Println("\n⏹️  Получен сигнал завершения, выключаемся...")

	// Останавливаем бота
	bot.Stop()

	// Останавливаем планировщик
	sch.Shutdown()

	// Закрываем БД
	database.Close()

	log.Println("✅ Бот успешно остановлен")
}

// Helper functions
func sscanf(str string, format string, args ...interface{}) (int, error) {
	return fmt.Sscanf(str, format, args...)
}

func parseDateTimeCallback(data string) []string {
	// Формат: 2024-01-15_14:30
	parts := make([]string, 0)
	if len(data) >= 16 {
		parts = append(parts, data[:10])   // дата
		parts = append(parts, data[11:16]) // время
	}
	return parts
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
