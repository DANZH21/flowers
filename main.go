package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
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
	go sch.RestoreTimers() // Восстанавливаем таймеры при запуске
	log.Println("✅ [ПЛАНИРОВЩК] Планировщик создан и таймеры восстановлены")

	// Создаем обработчики клиента и админа
	log.Println("🎯 [ИНИЦИАЛИЗАЦИЯ] Создание обработчиков...")
	clientHandler := handlers.NewClientHandler(database, bot, stateManager, sch, cfg.AdminIDs)
	adminHandler := handlers.NewAdminHandler(database, bot, stateManager, cfg.AdminIDs)
	log.Println("✅ [ОБРАБОТЧИКИ] Обработчики созданы")

	// ========== РЕГИСТРАЦИЯ КОМАНД В TELEGRAM API ==========
	log.Println("📝 [РЕГИСТРАЦИЯ] Регистрирую команды в Telegram API...")
	bot.SetCommands(
		&telebot.Command{Text: "/start", Description: "Главное меню"},
		&telebot.Command{Text: "/admin", Description: "Панель администратора"},
	)
	log.Println("✅ [РЕГИСТРАЦИЯ] Команды зарегистрированы")

	// ========== КОМАНДЫ ==========
	log.Println("📝 [РЕГИСТРАЦИЯ] Регистрирую handlers для команд...")

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

	// ========== INLINE BUTTON HANDLERS ==========
	// All inline buttons are handled through OnCallback handler below

	// ========== CALLBACK HANDLERS ==========

	// Выбор услуги
	bot.Handle(telebot.OnCallback, func(c telebot.Context) error {
		// В telebot v3 у инлайн кнопок Data лежит в c.Callback().Data (если payload пустой - там только Unique)
		// Убираем маркер telebot.v3 "\f" и любые другие непечатные символы
		data := c.Callback().Data
		data = strings.TrimLeft(data, "\f")
		data = strings.TrimSpace(data)
		userID := c.Sender().ID

		log.Printf("🔘 [CALLBACK] Очищенные данные: '%s'\n", data)

		// ========== ДИНАМИЧЕСКИЕ РОУТЫ ==========
		switch {
		case strings.HasPrefix(data, "select_service_"):
			var serviceID int
			fmt.Sscanf(data, "select_service_%d", &serviceID)
			c.Respond()
			return clientHandler.HandleSelectService(c, serviceID)

		case strings.HasPrefix(data, "select_date_"):
			dateStr := strings.TrimPrefix(data, "select_date_")
			c.Respond()
			return clientHandler.HandleSelectDate(c, dateStr)

		case strings.HasPrefix(data, "select_time_"):
			parts := parseDateTimeCallback(strings.TrimPrefix(data, "select_time_"))
			if len(parts) == 2 {
				c.Respond()
				return clientHandler.HandleSelectTime(c, parts[0], parts[1])
			}

		case strings.HasPrefix(data, "payment_kaspi_"):
			c.Respond()
			return clientHandler.HandlePaymentKaspi(c, userID)

		case strings.HasPrefix(data, "payment_cash_"):
			c.Respond()
			return clientHandler.HandlePaymentCash(c, userID)

		case strings.HasPrefix(data, "confirm_receipt_"):
			var aptID int
			fmt.Sscanf(data, "confirm_receipt_%d", &aptID)
			c.Respond()
			return adminHandler.HandleConfirmReceipt(c, aptID)

		case strings.HasPrefix(data, "reject_receipt_"):
			var aptID int
			fmt.Sscanf(data, "reject_receipt_%d", &aptID)
			c.Respond()
			return adminHandler.HandleRejectReceipt(c, aptID)

		case strings.HasPrefix(data, "admin_edit_service_"):
			var serviceID int
			fmt.Sscanf(data, "admin_edit_service_%d", &serviceID)
			c.Respond()
			return adminHandler.HandleAdminEditService(c, serviceID)

		case strings.HasPrefix(data, "admin_apt_detail_"):
			var aptID int
			fmt.Sscanf(data, "admin_apt_detail_%d", &aptID)
			c.Respond()
			return adminHandler.HandleAdminViewAppointmentDetail(c, aptID)

		case strings.HasPrefix(data, "edit_service_name_"):
			var serviceID int
			fmt.Sscanf(data, "edit_service_name_%d", &serviceID)
			c.Respond()
			session := stateManager.GetUserSession(userID)
			session.State = models.StateAdminEditServiceName
			session.TempData = map[string]interface{}{"serviceID": serviceID}
			stateManager.SetUserSession(userID, session)
			return c.Send("📝 Введите новое название услуги:")

		case strings.HasPrefix(data, "edit_service_price_"):
			var serviceID int
			fmt.Sscanf(data, "edit_service_price_%d", &serviceID)
			c.Respond()
			session := stateManager.GetUserSession(userID)
			session.State = models.StateAdminEditServicePrice
			session.TempData = map[string]interface{}{"serviceID": serviceID}
			stateManager.SetUserSession(userID, session)
			return c.Send("💰 Введите новую цену услуги (в тг):")

		case strings.HasPrefix(data, "edit_service_duration_"):
			var serviceID int
			fmt.Sscanf(data, "edit_service_duration_%d", &serviceID)
			c.Respond()
			session := stateManager.GetUserSession(userID)
			session.State = models.StateAdminEditServiceDuration
			session.TempData = map[string]interface{}{"serviceID": serviceID}
			stateManager.SetUserSession(userID, session)
			return c.Send("⏱️ Введите длительность услуги (в минутах):")

		case strings.HasPrefix(data, "edit_service_toggle_"):
			var serviceID int
			fmt.Sscanf(data, "edit_service_toggle_%d", &serviceID)
			c.Respond()
			ctx := context.Background()
			row := database.QueryRow(ctx, `SELECT is_available FROM services WHERE id = $1`, serviceID)
			var isAvail bool
			if err := row.Scan(&isAvail); err == nil {
				database.Exec(ctx, `UPDATE services SET is_available = $1 WHERE id = $2`, !isAvail, serviceID)
				if !isAvail {
					return c.Send("✅ Услуга активирована")
				}
				return c.Send("❌ Услуга деактивирована")
			}
			return c.Send("❌ Ошибка")

		case strings.HasPrefix(data, "edit_service_delete_"):
			var serviceID int
			fmt.Sscanf(data, "edit_service_delete_%d", &serviceID)
			c.Respond()
			ctx := context.Background()
			database.Exec(ctx, `DELETE FROM services WHERE id = $1`, serviceID)
			return c.Send("✅ Услуга удалена")

		case strings.HasPrefix(data, "apt_complete_"):
			var aptID int
			fmt.Sscanf(data, "apt_complete_%d", &aptID)
			c.Respond()
			database.Exec(context.Background(), `UPDATE appointments SET status = $1, updated_at = NOW() WHERE id = $2`, models.AppointmentStatusCompleted, aptID)
			return c.Send("✅ Запись завершена")

		case strings.HasPrefix(data, "apt_cancel_"):
			var aptID int
			fmt.Sscanf(data, "apt_cancel_%d", &aptID)
			c.Respond()
			database.Exec(context.Background(), `UPDATE appointments SET status = $1, updated_at = NOW() WHERE id = $2`, models.AppointmentStatusCancelled, aptID)
			return c.Send("❌ Запись отменена")
		}

		// ========== СТАТИЧНЫЕ РОУТЫ ==========
		switch data {
		case "book_appointment":
			c.Respond()
			return clientHandler.HandleBookAppointment(c)
		case "my_appointments":
			c.Respond()
			return clientHandler.HandleMyAppointments(c)
		case "about":
			c.Respond()
			return c.Send("ℹ️ *BEAUTY SALON*\n📍 Адрес: Алматы\n⏰ 10:00 - 20:00")
		case "support":
			c.Respond()
			return c.Send("📞 *Поддержка*\nTelegram: @salon_support")
		case "main_menu":
			c.Respond()
			return clientHandler.HandleStart(c)
		case "admin_appointments":
			c.Respond()
			return adminHandler.HandleAdminAppointments(c)
		case "admin_services":
			c.Respond()
			return adminHandler.HandleAdminServices(c)
		case "admin_settings":
			c.Respond()
			return adminHandler.HandleAdminSettings(c)
		case "admin_add_service":
			c.Respond()
			return adminHandler.HandleAdminAddService(c)
		case "admin_set_name":
			c.Respond()
			return adminHandler.HandleAdminSetName(c)
		case "admin_set_address":
			c.Respond()
			return adminHandler.HandleAdminSetAddress(c)
		case "admin_set_open":
			c.Respond()
			return adminHandler.HandleAdminSetScheduleOpen(c)
		case "admin_set_close":
			c.Respond()
			return adminHandler.HandleAdminSetScheduleClose(c)
		case "admin_set_reminder":
			c.Respond()
			return adminHandler.HandleAdminSetReminderHours(c)
		case "admin_set_prepay":
			c.Respond()
			return adminHandler.HandleAdminSetPrepayPercent(c)
		case "admin_set_support":
			c.Respond()
			return adminHandler.HandleAdminSetSupportID(c)
		case "admin_set_protection":
			c.Respond()
			return adminHandler.HandleAdminSetProtection(c)
		case "admin_stats":
			c.Respond()
			return c.Send("📊 Статистика (в разработке)")
		}

		// Если ничего не подошло
		log.Printf("⚠️ [CALLBACK] Неизвестный маршрут: '%s'\n", data)
		c.Respond()
		return c.Edit(fmt.Sprintf("❌ Ошибка: неизвестный маршурт '%s'", data))
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
		case models.StateAdminAddServiceName:
			return handleAddServiceName(c, stateManager)
		case models.StateAdminAddServicePrice:
			return handleAddServicePrice(c, stateManager)
		case models.StateAdminAddServiceDuration:
			return handleAddServiceDuration(c, stateManager, database)
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
		case models.StateAdminSetProtection:
			return adminHandler.HandleAdminInputProtection(c)
		case models.StateAdminEditServiceName:
			return handleEditServiceName(c, stateManager, database, adminHandler)
		case models.StateAdminEditServicePrice:
			return handleEditServicePrice(c, stateManager, database, adminHandler)
		case models.StateAdminEditServiceDuration:
			return handleEditServiceDuration(c, stateManager, database, adminHandler)
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

	// Обработчик для контактов
	bot.Handle(telebot.OnContact, func(c telebot.Context) error {
		userID := c.Sender().ID
		state := stateManager.GetState(userID)

		log.Printf("📱 [CONTACT] Пользователь %d отправил контакт (состояние %v)\n", userID, state)

		if state == models.StateAwaitingPhone {
			return clientHandler.HandlePhoneInput(c)
		}

		return c.Send("❌ В данный момент номер телефона не требуется")
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

// Обработчики добавления услуг
func handleAddServiceName(c telebot.Context, sm *handlers.StateManager) error {
	userID := c.Sender().ID
	name := strings.TrimSpace(c.Message().Text)

	if len(name) < 2 || len(name) > 100 {
		return c.Send("❌ Название должно быть от 2 до 100 символов")
	}

	session := sm.GetUserSession(userID)
	session.State = models.StateAdminAddServicePrice
	if session.TempData == nil {
		session.TempData = make(map[string]interface{})
	}
	session.TempData["newServiceName"] = name
	sm.SetUserSession(userID, session)

	return c.Send("💰 Введите цену услуги (в тг):")
}

func handleAddServicePrice(c telebot.Context, sm *handlers.StateManager) error {
	userID := c.Sender().ID
	priceStr := strings.TrimSpace(c.Message().Text)

	var price float64
	if _, err := fmt.Sscanf(priceStr, "%f", &price); err != nil || price <= 0 || price > 999999 {
		return c.Send("❌ Введите корректную цену (число от 1 до 999999):")
	}

	session := sm.GetUserSession(userID)
	session.State = models.StateAdminAddServiceDuration
	session.TempData["newServicePrice"] = price
	sm.SetUserSession(userID, session)

	return c.Send("⏱️ Введите длительность услуги (в минутах):")
}

func handleAddServiceDuration(c telebot.Context, sm *handlers.StateManager, database *db.Database) error {
	userID := c.Sender().ID
	durationStr := strings.TrimSpace(c.Message().Text)

	var duration int
	if _, err := fmt.Sscanf(durationStr, "%d", &duration); err != nil || duration < 5 || duration > 1440 {
		return c.Send("❌ Введите корректную длительность в минутах (от 5 до 1440):")
	}

	session := sm.GetUserSession(userID)
	name, _ := session.TempData["newServiceName"].(string)
	price, _ := session.TempData["newServicePrice"].(float64)

	ctx := context.Background()
	_, err := database.Exec(ctx, `INSERT INTO services (name, price, duration_min, is_available) VALUES ($1, $2, $3, true)`, name, price, duration)
	if err != nil {
		return c.Send("❌ Ошибка при сохранении услуги")
	}

	sm.ResetState(userID)
	return c.Send("✅ Услуга успешно добавлена!\nНажмите /admin для возврата в панель.")
}

// Обработчики редактирования услуг
func handleEditServiceName(c telebot.Context, sm *handlers.StateManager, database *db.Database, ah *handlers.AdminHandler) error {
	ctx := context.Background()
	userID := c.Sender().ID
	name := strings.TrimSpace(c.Message().Text)

	if len(name) < 2 || len(name) > 100 {
		return c.Send("❌ Название должно быть от 2 до 100 символов")
	}

	session := sm.GetUserSession(userID)
	serviceID, ok := session.TempData["serviceID"].(int)
	if !ok {
		return c.Send("❌ Ошибка: неверный ID услуги")
	}

	if _, err := database.Exec(ctx, `UPDATE services SET name = $1 WHERE id = $2`, name, serviceID); err != nil {
		return c.Send("❌ Ошибка при сохранении")
	}

	sm.ResetState(userID)
	return c.Send("✅ Название услуги обновлено")
}

func handleEditServicePrice(c telebot.Context, sm *handlers.StateManager, database *db.Database, ah *handlers.AdminHandler) error {
	ctx := context.Background()
	userID := c.Sender().ID
	priceStr := strings.TrimSpace(c.Message().Text)

	price := 0.0
	if _, err := fmt.Sscanf(priceStr, "%f", &price); err != nil || price <= 0 || price > 999999 {
		return c.Send("❌ Введите корректную цену (1-999999)")
	}

	session := sm.GetUserSession(userID)
	serviceID, ok := session.TempData["serviceID"].(int)
	if !ok {
		return c.Send("❌ Ошибка: неверный ID услуги")
	}

	if _, err := database.Exec(ctx, `UPDATE services SET price = $1 WHERE id = $2`, price, serviceID); err != nil {
		return c.Send("❌ Ошибка при сохранении")
	}

	sm.ResetState(userID)
	return c.Send(fmt.Sprintf("✅ Цена услуги обновлена: %g тг", price))
}

func handleEditServiceDuration(c telebot.Context, sm *handlers.StateManager, database *db.Database, ah *handlers.AdminHandler) error {
	ctx := context.Background()
	userID := c.Sender().ID
	durationStr := strings.TrimSpace(c.Message().Text)

	duration := 0
	if _, err := fmt.Sscanf(durationStr, "%d", &duration); err != nil || duration < 15 || duration > 480 {
		return c.Send("❌ Введите длительность от 15 до 480 минут")
	}

	session := sm.GetUserSession(userID)
	serviceID, ok := session.TempData["serviceID"].(int)
	if !ok {
		return c.Send("❌ Ошибка: неверный ID услуги")
	}

	if _, err := database.Exec(ctx, `UPDATE services SET duration_min = $1 WHERE id = $2`, duration, serviceID); err != nil {
		return c.Send("❌ Ошибка при сохранении")
	}

	sm.ResetState(userID)
	return c.Send(fmt.Sprintf("✅ Длительность услуги обновлена: %d мин", duration))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
