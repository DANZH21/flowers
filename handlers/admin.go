package handlers

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"flower-bot/db"
	"flower-bot/models"

	"gopkg.in/telebot.v3"
)

// AdminHandler обрабатывает запросы администраторов
type AdminHandler struct {
	db           *db.Database
	bot          *telebot.Bot
	stateManager *StateManager
	adminIDs     []int64
}

// NewAdminHandler создает новый обработчик админа
func NewAdminHandler(database *db.Database, bot *telebot.Bot, sm *StateManager, adminIDs []int64) *AdminHandler {
	return &AdminHandler{
		db:           database,
		bot:          bot,
		stateManager: sm,
		adminIDs:     adminIDs,
	}
}

// IsAdmin проверяет является ли пользователь админом
func (ah *AdminHandler) IsAdmin(userID int64) bool {
	for _, id := range ah.adminIDs {
		if id == userID {
			return true
		}
	}

	// Проверяем в БД
	ctx := context.Background()
	row := ah.db.QueryRow(ctx, "SELECT is_admin FROM users WHERE telegram_id = $1", userID)
	var isAdmin bool
	if err := row.Scan(&isAdmin); err == nil && isAdmin {
		return true
	}

	return false
}

// HandleAdminMenu показывает админ панель
func (ah *AdminHandler) HandleAdminMenu(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID

	if !ah.IsAdmin(userID) {
		return sendOrEdit(c, "❌ Доступ запрещён. Вы не администратор.")
	}

	log.Printf("📊 [АДМИН] Админ %d открыл панель\n", userID)

	// Получаем статистику
	salonSettings, err := ah.db.GetSalonSettings(ctx)
	salonName := "Beauty Salon"
	address := "Не указан"
	if err == nil {
		if name, ok := salonSettings["salon_name"].(string); ok {
			salonName = name
		}
		if addr, ok := salonSettings["address"].(string); ok {
			address = addr
		}
	}

	// Активные записи
	pendingRow := ah.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM appointments WHERE status IN ('scheduled', 'confirmed')`)
	var pendingCount int
	pendingRow.Scan(&pendingCount)

	// Услуги
	serviceRow := ah.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM services WHERE is_available = true`)
	var serviceCount int
	serviceRow.Scan(&serviceCount)

	// Пользователи
	usersRow := ah.db.QueryRow(ctx, `SELECT COUNT(*) FROM users`)
	var usersCount int
	usersRow.Scan(&usersCount)

	// Текст панели
	text := fmt.Sprintf(
		"💅 АДМИН ПАНЕЛЬ 💅\n\n"+
			"Салон: %s\n"+
			"Адрес: %s\n\n"+
			"📊 СТАТИСТИКА:\n"+
			"📅 Активные записи: %d\n"+
			"💅 Услуг в каталоге: %d\n"+
			"👥 Зарегистрировано клиентов: %d\n\n"+
			"⚡ БЫСТРЫЕ ДЕЙСТВИЯ:\n",
		salonName, address, pendingCount, serviceCount, usersCount)

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			menu.Data("📅 Записи", "admin_appointments"),
			menu.Data("💅 Услуги", "admin_services"),
		),
		menu.Row(
			menu.Data("⚙️ Настройки", "admin_settings"),
			menu.Data("📊 Статистика", "admin_stats"),
		),
		menu.Row(
			menu.Data("📣 Массовая рассылка", "admin_broadcast"),
		),
	)

	log.Printf("✅ [ADMIN] Показываю панель настроек\n")
	return sendOrEdit(c, text, menu)
}

// ========== СТАТИСТИКА ==========

// HandleAdminStats показывает статистику салона за период (today, week, month, all)
func (ah *AdminHandler) HandleAdminStats(c telebot.Context, period string) error {
	ctx := context.Background()

	var totalUsers, totalAppointments, completedAppointments int
	var totalRevenue float64
	var periodLabel string
	var dateFilter string

	switch period {
	case "today":
		periodLabel = "за ТУДЭЙ (сегодня)"
		dateFilter = "AND created_at >= CURRENT_DATE"
	case "week":
		periodLabel = "за НЕДЕЛЮ"
		dateFilter = "AND created_at >= CURRENT_DATE - INTERVAL '7 days'"
	case "month":
		periodLabel = "за МЕСЯЦ"
		dateFilter = "AND created_at >= CURRENT_DATE - INTERVAL '1 month'"
	default:
		periodLabel = "за ВСЁ ВРЕМЯ"
		dateFilter = ""
	}

	// Клиенты за период (если надо всех, то убираем дату)
	if period == "all" {
		ah.db.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&totalUsers)
	} else {
		ah.db.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM users WHERE 1=1 %s", dateFilter)).Scan(&totalUsers)
	}

	ah.db.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM appointments WHERE 1=1 %s", dateFilter)).Scan(&totalAppointments)
	ah.db.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM appointments WHERE status = 'completed' %s", dateFilter)).Scan(&completedAppointments)
	ah.db.QueryRow(ctx, fmt.Sprintf("SELECT COALESCE(SUM(amount), 0) FROM appointments WHERE status = 'completed' %s", dateFilter)).Scan(&totalRevenue)

	text := fmt.Sprintf(
		"📊 <b>CRM: Статистика салона %s</b>\n\n"+
			"👥 Новых клиентов: <b>%d</b>\n"+
			"📅 Всего записей: <b>%d</b>\n"+
			"✅ Завершено (оплачено): <b>%d</b>\n\n"+
			"💰 Выручка: <b>%.2f ₸</b>\n",
		periodLabel, totalUsers, totalAppointments, completedAppointments, totalRevenue)

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			menu.Data("📅 Сегодня", "admin_stats_today"),
			menu.Data("📆 Неделя", "admin_stats_week"),
		),
		menu.Row(
			menu.Data("🗓 Месяц", "admin_stats_month"),
			menu.Data("🌍 Всё время", "admin_stats_all"),
		),
		menu.Row(
			menu.Data("🔙 Назад", "admin_menu"),
		),
	)

	if c.Callback() != nil {
		return c.Edit(text, telebot.ModeHTML, menu)
	}
	return sendOrEdit(c, text, telebot.ModeHTML, menu)
}

// ========== РАССЫЛКА ==========

// HandleAdminBroadcast начинает массовую рассылку
func (ah *AdminHandler) HandleAdminBroadcast(c telebot.Context) error {
	userID := c.Sender().ID

	session := ah.stateManager.GetUserSession(userID)
	session.State = models.StateAdminBroadcast
	ah.stateManager.SetUserSession(userID, session)

	text := "📣 <b>Массовая рассылка</b>\n\nОтправьте текст или фото, которое хотите разослать всем зарегистрированным клиентам. Рассылку получат все пользователи из базы.\n\n" +
		"<i>Для отмены нажмите /cancel.</i>"

	menu := &telebot.ReplyMarkup{}
	menu.Inline(menu.Row(menu.Data("❌ Отмена", "admin_menu")))

	msg, err := ah.bot.Send(c.Sender(), text, telebot.ModeHTML, menu)
	if err == nil {
		ah.stateManager.AddMessageToDelete(userID, msg.ID)
	}
	return err
}

// HandleAdminInputBroadcast обрабатывает и отправляет рассылку
func (ah *AdminHandler) HandleAdminInputBroadcast(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID

	ah.stateManager.ResetState(userID)

	rows, err := ah.db.Query(ctx, "SELECT telegram_id FROM users")
	if err != nil {
		return sendOrEdit(c, "❌ Ошибка получения пользователей базы")
	}
	defer rows.Close()

	var userIDs []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err == nil {
			userIDs = append(userIDs, uid)
		}
	}

	go func() {
		successCount := 0
		failCount := 0
		for _, uid := range userIDs {
			_, err := ah.bot.Copy(&telebot.User{ID: uid}, c.Message())
			if err != nil {
				failCount++
				log.Printf("❌ Не удалось отправить рассылку %d: %v", uid, err)
			} else {
				successCount++
			}
		}
		ah.bot.Send(&telebot.User{ID: userID}, fmt.Sprintf("✅ <b>Рассылка завершена!</b>\n\nУспешно доставлено: <b>%d</b>\nЗаблокировали бота: <b>%d</b>", successCount, failCount), telebot.ModeHTML)
	}()

	return sendOrEdit(c, "⏳ Запущена массовая рассылка. Вы получите уведомление по её завершении.")
}

// ========== ЗАПИСИ ==========

// HandleAdminAppointments показывает список записей
func (ah *AdminHandler) HandleAdminAppointments(c telebot.Context) error {
	ctx := context.Background()

	log.Printf("📅 [ADMIN] Запрос списка записей\n")

	rows, err := ah.db.Query(ctx,
		`SELECT a.id, a.appointment_num, a.customer_name, a.status, a.appointment_time
		FROM appointments a
		WHERE a.status IN ($1, $2)
		ORDER BY a.appointment_time ASC
		LIMIT 20`,
		models.AppointmentStatusScheduled, models.AppointmentStatusConfirmed)
	if err != nil {
		log.Printf("❌ [ADMIN] Ошибка получения записей: %v\n", err)
		return sendOrEdit(c, "❌ Ошибка при загрузке записей")
	}
	defer rows.Close()

	menu := &telebot.ReplyMarkup{}
	var btnRows []telebot.Row

	for rows.Next() {
		var id, appointmentNum int
		var customerNamePtr *string
		var status string
		var appointmentTime time.Time

		if err := rows.Scan(&id, &appointmentNum, &customerNamePtr, &status, &appointmentTime); err != nil {
			log.Printf("❌ Ошибка сканирования записи: %v\n", err)
			continue
		}

		customerName := "Неизвестный клиент"
		if customerNamePtr != nil {
			customerName = *customerNamePtr
		}

		statusEmoji := map[string]string{
			models.AppointmentStatusScheduled: "📅",
			models.AppointmentStatusConfirmed: "✅",
			models.AppointmentStatusCompleted: "✔️",
			models.AppointmentStatusCancelled: "❌",
		}[status]

		btnRows = append(btnRows, menu.Row(
			menu.Data(
				fmt.Sprintf("%s #%d — %s — %s", statusEmoji, appointmentNum, customerName, appointmentTime.Format("02/01 15:04")),
				fmt.Sprintf("admin_apt_detail_%d", id),
			),
		))
	}

	if len(btnRows) > 0 {
		menu.Inline(btnRows...)
	}

	log.Printf("✅ [ADMIN] Показываю список записей\n")
	return sendOrEdit(c, "📅 Активные записи:", menu)
}

// HandleAdminServices показывает список услуг
func (ah *AdminHandler) HandleAdminServices(c telebot.Context) error {
	ctx := context.Background()

	log.Printf("💅 [ADMIN] Запрос списка услуг\n")

	log.Printf("💅 [ADMIN] Запрос списка услуг\n")

	rows, err := ah.db.Query(ctx,
		`SELECT id, name, price, duration_min, is_available FROM services ORDER BY name`)
	if err != nil {
		log.Printf("❌ Ошибка получения услуг: %v\n", err)
		return sendOrEdit(c, "❌ Ошибка при загрузке услуг")
	}
	defer rows.Close()

	text := "💅 УСЛУГИ:\n\n"
	menu := &telebot.ReplyMarkup{}
	var btnRows []telebot.Row

	for rows.Next() {
		var id, duration int
		var name string
		var price float64
		var isAvailable bool

		if err := rows.Scan(&id, &name, &price, &duration, &isAvailable); err != nil {
			log.Printf("❌ Ошибка сканирования услуги: %v\n", err)
			continue
		}

		availability := "✅"
		if !isAvailable {
			availability = "❌"
		}

		text += fmt.Sprintf("%s %s • %g тг • %d мин\n", availability, name, price, duration)

		btn := menu.Data(
			fmt.Sprintf("✏️ %s", name),
			fmt.Sprintf("admin_edit_service_%d", id),
		)
		btnRows = append(btnRows, menu.Row(btn))
	}

	btnRows = append(btnRows, menu.Row(
		menu.Data("➕ Добавить услугу", "admin_add_service"),
	))

	menu.Inline(btnRows...)

	log.Printf("✅ [ADMIN] Показываю %d услуг\n", len(btnRows)-1)
	return sendOrEdit(c, text, menu)
}

// HandleAdminAddService начинает добавление услуги
func (ah *AdminHandler) HandleAdminAddService(c telebot.Context) error {
	userID := c.Sender().ID

	// Устанавливаем состояние
	session := ah.stateManager.GetUserSession(userID)
	session.State = models.StateAdminAddServiceName
	ah.stateManager.SetUserSession(userID, session)

	text := "📝 Введите название услуги:"
	msg, err := ah.bot.Send(c.Sender(), text)
	if err == nil {
		ah.stateManager.AddMessageToDelete(userID, msg.ID)
	}

	return err
}

// HandleAdminSettings показывает панель настроек
func (ah *AdminHandler) HandleAdminSettings(c telebot.Context) error {
	ctx := context.Background()

	log.Printf("⚙️ [ADMIN] Запрос настроек салона\n")

	salonSettings, err := ah.db.GetSalonSettings(ctx)
	if err != nil {
		log.Printf("❌ [ADMIN] Ошибка получения настроек: %v\n", err)
		return sendOrEdit(c, "❌ Ошибка")
	}

	protectionText := "❌ Выкл"
	if enabled, ok := salonSettings["protection_enabled"].(bool); ok && enabled {
		protectionText = fmt.Sprintf("✅ ВКЛ (мин. %d заказ(ов))", salonSettings["protection_min_orders"])
	}

	text := fmt.Sprintf(
		"⚙️ НАСТРОЙКИ САЛОНА\n\n"+
			"Название: %s\n"+
			"Адрес: %s\n"+
			"📅 Рабочее время: %s - %s\n"+
			"🔔 Напоминание за: %d ч\n"+
			"💳 Предоплата: %d%%\n"+
			"🛡 Защита (наличные после N): %s\n",
		salonSettings["salon_name"], salonSettings["address"],
		salonSettings["schedule_open"], salonSettings["schedule_close"],
		salonSettings["reminder_hours"], salonSettings["prepay_percent"],
		protectionText)

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			menu.Data("📝 Название", "admin_set_name"),
			menu.Data("📍 Адрес", "admin_set_address"),
		),
		menu.Row(
			menu.Data("🕐 Время открытия", "admin_set_open"),
			menu.Data("🕐 Время закрытия", "admin_set_close"),
		),
		menu.Row(
			menu.Data("🔔 Напоминание", "admin_set_reminder"),
			menu.Data("💳 Предоплата %", "admin_set_prepay"),
		),
		menu.Row(
			menu.Data("🛡 Настроить защиту", "admin_set_protection"),
			menu.Data("📸 Портфолио", "admin_set_channel"),
		),
		menu.Row(
			menu.Data("💳 Kaspi Ссылка", "admin_set_kaspi"),
		),
		menu.Row(
			menu.Data("💬 Поддержка ID", "admin_set_support"),
			menu.Data("🏠 Меню", "main_menu"),
		),
	)

	log.Printf("✅ [ADMIN] Показываю панель настроек\n")
	return sendOrEdit(c, text, menu)
}

// HandleAdminSetKaspi начинает изменение ссылки Kaspi
func (ah *AdminHandler) HandleAdminSetKaspi(c telebot.Context) error {
	userID := c.Sender().ID

	session := ah.stateManager.GetUserSession(userID)
	session.State = models.StateAdminSetKaspiLink
	ah.stateManager.SetUserSession(userID, session)

	text := "💳 Введите новую ссылку на оплату Kaspi (начинается с `https://pay.kaspi.kz/`):"
	msg, err := ah.bot.Send(c.Sender(), text)
	if err == nil {
		ah.stateManager.AddMessageToDelete(userID, msg.ID)
	}
	return err
}

// HandleAdminSetChannel начинает изменение ссылки на канал/портфолио
func (ah *AdminHandler) HandleAdminSetChannel(c telebot.Context) error {
	userID := c.Sender().ID

	session := ah.stateManager.GetUserSession(userID)
	session.State = models.StateAdminSetChannelLink
	ah.stateManager.SetUserSession(userID, session)

	text := "📸 Введите ссылку на канал или портфолио (например, `https://t.me/my_salon`):"
	msg, err := ah.bot.Send(c.Sender(), text)
	if err == nil {
		ah.stateManager.AddMessageToDelete(userID, msg.ID)
	}
	return err
}

// HandleAdminSetProtection начинает настройку защиты
func (ah *AdminHandler) HandleAdminSetProtection(c telebot.Context) error {
	userID := c.Sender().ID

	session := ah.stateManager.GetUserSession(userID)
	session.State = models.StateAdminSetProtection
	ah.stateManager.SetUserSession(userID, session)

	text := "🛡 Введите количество успешных записей для разрешения оплаты наличными (например, `3`).\n\nВведите `0`, чтобы отключить защиту (разрешить всем)."
	msg, err := ah.bot.Send(c.Sender(), text, telebot.ModeMarkdown)
	if err == nil {
		ah.stateManager.AddMessageToDelete(userID, msg.ID)
	}
	return err
}

// HandleAdminSetName начинает изменение названия
func (ah *AdminHandler) HandleAdminSetName(c telebot.Context) error {
	userID := c.Sender().ID

	session := ah.stateManager.GetUserSession(userID)
	session.State = models.StateAdminSetSalonName
	ah.stateManager.SetUserSession(userID, session)

	text := "📝 Введите новое название салона:"
	msg, err := ah.bot.Send(c.Sender(), text)
	if err == nil {
		ah.stateManager.AddMessageToDelete(userID, msg.ID)
	}

	return err
}

// HandleAdminSetAddress начинает изменение адреса
func (ah *AdminHandler) HandleAdminSetAddress(c telebot.Context) error {
	userID := c.Sender().ID

	session := ah.stateManager.GetUserSession(userID)
	session.State = models.StateAdminSetAddress
	ah.stateManager.SetUserSession(userID, session)

	text := "📍 Введите адрес салона:"
	msg, err := ah.bot.Send(c.Sender(), text)
	if err == nil {
		ah.stateManager.AddMessageToDelete(userID, msg.ID)
	}

	return err
}

// HandleAdminInputSalonName обрабатывает ввод названия
func (ah *AdminHandler) HandleAdminInputSalonName(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID
	name := strings.TrimSpace(c.Message().Text)

	if len(name) < 2 || len(name) > 100 {
		return sendOrEdit(c, "❌ Название должно быть от 2 до 100 символов")
	}

	// Обновляем в БД
	_, err := ah.db.Exec(ctx,
		`UPDATE salon_settings SET salon_name = $1`, name)
	if err != nil {
		log.Printf("❌ Ошибка обновления названия: %v\n", err)
		return sendOrEdit(c, "❌ Ошибка при сохранении")
	}

	ah.stateManager.ResetState(userID)

	text := fmt.Sprintf("✅ Название салона изменено на: %s", name)
	return sendOrEdit(c, text)
}

// HandleAdminInputAddress обрабатывает ввод адреса
func (ah *AdminHandler) HandleAdminInputAddress(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID
	address := strings.TrimSpace(c.Message().Text)

	if len(address) < 2 || len(address) > 200 {
		return sendOrEdit(c, "❌ Адрес должен быть от 2 до 200 символов")
	}

	// Обновляем в БД
	_, err := ah.db.Exec(ctx,
		`UPDATE salon_settings SET address = $1`, address)
	if err != nil {
		log.Printf("❌ Ошибка обновления адреса: %v\n", err)
		return sendOrEdit(c, "❌ Ошибка при сохранении")
	}

	ah.stateManager.ResetState(userID)

	text := fmt.Sprintf("✅ Адрес салона изменен на: %s", address)
	return sendOrEdit(c, text)
}

// HandleAdminSetScheduleOpen начинает изменение времени открытия
func (ah *AdminHandler) HandleAdminSetScheduleOpen(c telebot.Context) error {
	userID := c.Sender().ID

	session := ah.stateManager.GetUserSession(userID)
	session.State = models.StateAdminSetScheduleOpen
	ah.stateManager.SetUserSession(userID, session)

	text := "🕐 Введите время открытия (формат: 10:00):"
	msg, err := ah.bot.Send(c.Sender(), text)
	if err == nil {
		ah.stateManager.AddMessageToDelete(userID, msg.ID)
	}

	return err
}

// HandleAdminInputScheduleOpen обрабатывает ввод времени открытия
func (ah *AdminHandler) HandleAdminInputScheduleOpen(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID
	timeStr := strings.TrimSpace(c.Message().Text)

	// Проверяем формат
	if !isValidTimeFormat(timeStr) {
		return sendOrEdit(c, "❌ Неверный формат. Используйте: 10:00")
	}

	// Обновляем в БД
	_, err := ah.db.Exec(ctx,
		`UPDATE salon_settings SET schedule_open = $1`, timeStr)
	if err != nil {
		log.Printf("❌ Ошибка обновления времени: %v\n", err)
		return sendOrEdit(c, "❌ Ошибка при сохранении")
	}

	ah.stateManager.ResetState(userID)

	text := fmt.Sprintf("✅ Время открытия установлено: %s", timeStr)
	return sendOrEdit(c, text)
}

// HandleAdminSetScheduleClose начинает изменение времени закрытия
func (ah *AdminHandler) HandleAdminSetScheduleClose(c telebot.Context) error {
	userID := c.Sender().ID

	session := ah.stateManager.GetUserSession(userID)
	session.State = models.StateAdminSetScheduleClose
	ah.stateManager.SetUserSession(userID, session)

	text := "🕐 Введите время закрытия (формат: 20:00):"
	msg, err := ah.bot.Send(c.Sender(), text)
	if err == nil {
		ah.stateManager.AddMessageToDelete(userID, msg.ID)
	}

	return err
}

// HandleAdminInputScheduleClose обрабатывает ввод времени закрытия
func (ah *AdminHandler) HandleAdminInputScheduleClose(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID
	timeStr := strings.TrimSpace(c.Message().Text)

	// Проверяем формат
	if !isValidTimeFormat(timeStr) {
		return sendOrEdit(c, "❌ Неверный формат. Используйте: 20:00")
	}

	// Обновляем в БД
	_, err := ah.db.Exec(ctx,
		`UPDATE salon_settings SET schedule_close = $1`, timeStr)
	if err != nil {
		log.Printf("❌ Ошибка обновления времени: %v\n", err)
		return sendOrEdit(c, "❌ Ошибка при сохранении")
	}

	ah.stateManager.ResetState(userID)

	text := fmt.Sprintf("✅ Время закрытия установлено: %s", timeStr)
	return sendOrEdit(c, text)
}

// HandleAdminSetReminderHours начинает изменение напоминания
func (ah *AdminHandler) HandleAdminSetReminderHours(c telebot.Context) error {
	userID := c.Sender().ID

	session := ah.stateManager.GetUserSession(userID)
	session.State = models.StateAdminSetReminderHours
	ah.stateManager.SetUserSession(userID, session)

	text := "🔔 Введите за сколько часов отправлять напоминание (1-24):"
	msg, err := ah.bot.Send(c.Sender(), text)
	if err == nil {
		ah.stateManager.AddMessageToDelete(userID, msg.ID)
	}

	return err
}

// HandleAdminInputReminderHours обрабатывает ввод часов напоминания
func (ah *AdminHandler) HandleAdminInputReminderHours(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID
	input := strings.TrimSpace(c.Message().Text)

	hours := 0
	_, err := fmt.Sscanf(input, "%d", &hours)
	if err != nil || hours < 1 || hours > 24 {
		return sendOrEdit(c, "❌ Введите число от 1 до 24")
	}

	// Обновляем в БД
	_, err = ah.db.Exec(ctx,
		`UPDATE salon_settings SET reminder_hours = $1`, hours)
	if err != nil {
		log.Printf("❌ Ошибка обновления напоминания: %v\n", err)
		return sendOrEdit(c, "❌ Ошибка при сохранении")
	}

	ah.stateManager.ResetState(userID)

	text := fmt.Sprintf("✅ Напоминание установлено за %d ч перед записью", hours)
	return sendOrEdit(c, text)
}

// HandleAdminSetPrepayPercent начинает изменение % предоплаты
func (ah *AdminHandler) HandleAdminSetPrepayPercent(c telebot.Context) error {
	userID := c.Sender().ID

	session := ah.stateManager.GetUserSession(userID)
	session.State = models.StateAdminSetPrepayPercent
	ah.stateManager.SetUserSession(userID, session)

	text := "💳 Введите % предоплаты (0 для отключения, 1-100):"
	msg, err := ah.bot.Send(c.Sender(), text)
	if err == nil {
		ah.stateManager.AddMessageToDelete(userID, msg.ID)
	}

	return err
}

// HandleAdminInputPrepayPercent обрабатывает ввод % предоплаты
func (ah *AdminHandler) HandleAdminInputPrepayPercent(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID
	input := strings.TrimSpace(c.Message().Text)

	percent := 0
	_, err := fmt.Sscanf(input, "%d", &percent)
	if err != nil || percent < 0 || percent > 100 {
		return sendOrEdit(c, "❌ Введите число от 0 до 100")
	}

	// Обновляем в БД
	_, err = ah.db.Exec(ctx,
		`UPDATE salon_settings SET prepay_percent = $1`, percent)
	if err != nil {
		log.Printf("❌ Ошибка обновления предоплаты: %v\n", err)
		return sendOrEdit(c, "❌ Ошибка при сохранении")
	}

	ah.stateManager.ResetState(userID)

	status := "отключена"
	if percent > 0 {
		status = fmt.Sprintf("установлена на %d%%", percent)
	}
	text := fmt.Sprintf("✅ Предоплата %s", status)
	return sendOrEdit(c, text)
}

// HandleAdminSetSupportID начинает установку ID поддержки
func (ah *AdminHandler) HandleAdminSetSupportID(c telebot.Context) error {
	userID := c.Sender().ID

	session := ah.stateManager.GetUserSession(userID)
	session.State = models.StateAdminSetSupportID
	ah.stateManager.SetUserSession(userID, session)

	text := "💬 Введите Telegram ID для отправки уведомлений (ваш ID: " + fmt.Sprintf("%d", userID) + "):"
	msg, err := ah.bot.Send(c.Sender(), text)
	if err == nil {
		ah.stateManager.AddMessageToDelete(userID, msg.ID)
	}

	return err
}

// HandleAdminInputSupportID обрабатывает ввод ID поддержки
func (ah *AdminHandler) HandleAdminInputSupportID(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID
	input := strings.TrimSpace(c.Message().Text)

	supportID := int64(0)
	_, err := fmt.Sscanf(input, "%d", &supportID)
	if err != nil || supportID <= 0 {
		return sendOrEdit(c, "❌ Введите корректный Telegram ID (число)")
	}

	// Обновляем в БД
	_, err = ah.db.Exec(ctx,
		`UPDATE salon_settings SET support_user_id = $1`, supportID)
	if err != nil {
		log.Printf("❌ Ошибка обновления ID поддержки: %v\n", err)
		return sendOrEdit(c, "❌ Ошибка при сохранении")
	}

	ah.stateManager.ResetState(userID)

	text := fmt.Sprintf("✅ ID поддержки установлен: %d", supportID)
	return sendOrEdit(c, text)
}

// HandleAdminInputProtection обрабатывает ввод для защиты
func (ah *AdminHandler) HandleAdminInputProtection(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID
	input := strings.TrimSpace(c.Message().Text)

	minOrders := 0
	_, err := fmt.Sscanf(input, "%d", &minOrders)
	if err != nil || minOrders < 0 {
		return sendOrEdit(c, "❌ Введите корректное число (0 или больше)")
	}

	enabled := minOrders > 0

	_, err = ah.db.Exec(ctx,
		`UPDATE salon_settings SET protection_enabled = $1, protection_min_orders = $2`,
		enabled, minOrders)
	if err != nil {
		log.Printf("❌ Ошибка обновления защиты: %v\n", err)
		return sendOrEdit(c, "❌ Ошибка при сохранении")
	}

	ah.stateManager.ResetState(userID)

	if enabled {
		return sendOrEdit(c, fmt.Sprintf("✅ Защита ВКЛЮЧЕНА.\nОплата наличными доступна только после %d успешно завершенных заказов.", minOrders))
	}
	return sendOrEdit(c, "✅ Защита ОТКЛЮЧЕНА.\nВсе клиенты могут оплачивать наличными.")
}

// HandleAdminInputKaspi обрабатывает ввод ссылки на Kaspi
func (ah *AdminHandler) HandleAdminInputKaspi(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID
	link := strings.TrimSpace(c.Message().Text)

	if !strings.HasPrefix(link, "http") {
		return sendOrEdit(c, "❌ Ссылка должна начинаться с http:// или https://")
	}

	_, err := ah.db.Exec(ctx, `UPDATE salon_settings SET kaspi_link = $1`, link)
	if err != nil {
		log.Printf("❌ Ошибка обновления Kaspi: %v\n", err)
		return sendOrEdit(c, "❌ Ошибка при сохранении")
	}

	ah.stateManager.ResetState(userID)

	return sendOrEdit(c, fmt.Sprintf("✅ Ссылка Kaspi обновлена:\n%s", link))
}

// HandleAdminInputChannel обрабатывает ввод ссылки на портфолио
func (ah *AdminHandler) HandleAdminInputChannel(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID
	link := strings.TrimSpace(c.Message().Text)

	if !strings.HasPrefix(link, "http") {
		return sendOrEdit(c, "❌ Ссылка должна начинаться с http:// или https://")
	}

	_, err := ah.db.Exec(ctx, `UPDATE salon_settings SET about_channel_link = $1`, link)
	if err != nil {
		log.Printf("❌ Ошибка обновления канала: %v\n", err)
		return sendOrEdit(c, "❌ Ошибка при сохранении")
	}

	ah.stateManager.ResetState(userID)

	return sendOrEdit(c, fmt.Sprintf("✅ Ссылка на портфолио обновлена:\n%s", link))
}

// Helper function
func isValidTimeFormat(timeStr string) bool {
	if len(timeStr) != 5 || timeStr[2] != ':' {
		return false
	}
	h, m := 0, 0
	n, _ := fmt.Sscanf(timeStr, "%d:%d", &h, &m)
	return n == 2 && h >= 0 && h < 24 && m >= 0 && m < 60
}

// ========== ПОДТВЕРЖДЕНИЕ ЧЕКОВ ==========

// HandleConfirmReceipt подтверждает чек администратором
func (ah *AdminHandler) HandleConfirmReceipt(c telebot.Context, appointmentID int) error {
	ctx := context.Background()

	log.Printf("✅ [ЧЕК] Админ подтвердил чек для записи %d\n", appointmentID)

	// Получаем информацию о записи
	row := ah.db.QueryRow(ctx,
		`SELECT user_id, appointment_num, amount FROM appointments WHERE id = $1`, appointmentID)

	var userID int64
	var appointmentNum int
	var amount float64

	if err := row.Scan(&userID, &appointmentNum, &amount); err != nil {
		log.Printf("❌ Ошибка получения записи: %v\n", err)
		return sendOrEdit(c, "❌ Запись не найдена")
	}

	// Обновляем статус чека
	_, err := ah.db.Exec(ctx,
		`UPDATE appointments SET receipt_status = $1, updated_at = NOW() WHERE id = $2`,
		models.ReceiptStatusConfirmed, appointmentID)
	if err != nil {
		log.Printf("❌ Ошибка обновления статуса чека: %v\n", err)
		return sendOrEdit(c, "❌ Ошибка при подтверждении")
	}

	log.Printf("✅ Чек для записи #%d подтвержден\n", appointmentNum)

	// Отправляем уведомление клиенту
	user := &telebot.User{ID: userID}
	msg := fmt.Sprintf("✅ Ваш чек #%d подтвержден!\n\nЗапись на услугу подтверждена.", appointmentNum)
	if _, err := ah.bot.Send(user, msg); err != nil {
		log.Printf("⚠️ Ошибка отправки подтверждения клиенту: %v\n", err)
	}

	return sendOrEdit(c, fmt.Sprintf("✅ Чек #%d подтвержден", appointmentNum))
}

// HandleRejectReceipt отклоняет чек администратором
func (ah *AdminHandler) HandleRejectReceipt(c telebot.Context, appointmentID int) error {
	ctx := context.Background()

	log.Printf("❌ [ЧЕК] Админ отклонил чек для записи %d\n", appointmentID)

	// Получаем информацию о записи
	row := ah.db.QueryRow(ctx,
		`SELECT user_id, appointment_num FROM appointments WHERE id = $1`, appointmentID)

	var userID int64
	var appointmentNum int

	if err := row.Scan(&userID, &appointmentNum); err != nil {
		log.Printf("❌ Ошибка получения записи: %v\n", err)
		return sendOrEdit(c, "❌ Запись не найдена")
	}

	// Обновляем статус чека
	_, err := ah.db.Exec(ctx,
		`UPDATE appointments SET receipt_status = $1, updated_at = NOW() WHERE id = $2`,
		models.ReceiptStatusRejected, appointmentID)
	if err != nil {
		log.Printf("❌ Ошибка обновления статуса чека: %v\n", err)
		return sendOrEdit(c, "❌ Ошибка при отклонении")
	}

	log.Printf("✅ Чек для записи #%d отклонен\n", appointmentNum)

	// Отправляем уведомление клиенту
	user := &telebot.User{ID: userID}
	msg := fmt.Sprintf("❌ Ваш чек #%d был отклонен.\n\nПожалуйста, загрузите новый чек. У вас есть 30 минут.", appointmentNum)
	if _, err := ah.bot.Send(user, msg); err != nil {
		log.Printf("⚠️ Ошибка отправки отклонения клиенту: %v\n", err)
	}

	return sendOrEdit(c, fmt.Sprintf("❌ Чек #%d отклонен", appointmentNum))
}

// ========== РЕДАКТИРОВАНИЕ УСЛУГИ ==========

// HandleAdminEditService показывает форму редактирования услуги
func (ah *AdminHandler) HandleAdminEditService(c telebot.Context, serviceID int) error {
	ctx := context.Background()

	log.Printf("✏️ [ADMIN] Редактирование услуги %d\n", serviceID)

	// Получаем данные услуги
	row := ah.db.QueryRow(ctx,
		`SELECT id, name, price, duration_min, is_available FROM services WHERE id = $1`, serviceID)

	var id, duration int
	var name string
	var price float64
	var isAvailable bool

	if err := row.Scan(&id, &name, &price, &duration, &isAvailable); err != nil {
		log.Printf("❌ Ошибка получения услуги: %v\n", err)
		return sendOrEdit(c, "❌ Услуга не найдена")
	}

	availability := "✅ Доступна"
	if !isAvailable {
		availability = "❌ Недоступна"
	}

	text := fmt.Sprintf(
		"✏️ РЕДАКТИРОВАНИЕ УСЛУГИ\n\n"+
			"📝 Название: %s\n"+
			"💰 Цена: %g тг\n"+
			"⏱️ Длительность: %d мин\n"+
			"📊 Статус: %s\n",
		name, price, duration, availability)

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			menu.Data("📝 Название", fmt.Sprintf("edit_service_name_%d", id)),
			menu.Data("💰 Цена", fmt.Sprintf("edit_service_price_%d", id)),
		),
		menu.Row(
			menu.Data("⏱️ Длительность", fmt.Sprintf("edit_service_duration_%d", id)),
			menu.Data("📊 Статус", fmt.Sprintf("edit_service_toggle_%d", id)),
		),
		menu.Row(
			menu.Data("🗑️ Удалить", fmt.Sprintf("edit_service_delete_%d", id)),
			menu.Data("🏠 Меню", "admin_services"),
		),
	)

	return sendOrEdit(c, text, menu)
}

// ========== ДЕТАЛИ ЗАПИСИ ==========

// HandleAdminViewAppointmentDetail показывает детали записи
func (ah *AdminHandler) HandleAdminViewAppointmentDetail(c telebot.Context, appointmentID int) error {
	ctx := context.Background()

	log.Printf("📋 [ADMIN] Просмотр деталей записи %d\n", appointmentID)

	// Получаем данные записи
	row := ah.db.QueryRow(ctx,
		`SELECT a.id, a.appointment_num, a.customer_name, a.customer_phone, 
		        a.appointment_time, a.status, a.amount, a.payment_type,
		        s.name, a.receipt_status, a.receipt_url
		 FROM appointments a
		 JOIN services s ON a.service_id = s.id
		 WHERE a.id = $1`, appointmentID)

	var id, appointmentNum int
	var customerNamePtr, customerPhonePtr, paymentTypePtr, receiptStatusPtr *string
	var serviceName, status string
	var appointmentTime time.Time
	var amount float64
	var receiptURL *string

	if err := row.Scan(&id, &appointmentNum, &customerNamePtr, &customerPhonePtr, &appointmentTime,
		&status, &amount, &paymentTypePtr, &serviceName, &receiptStatusPtr, &receiptURL); err != nil {
		log.Printf("❌ Ошибка получения записи: %v\n", err)
		return sendOrEdit(c, "❌ Запись не найдена")
	}

	customerName := "Неизвестный"
	if customerNamePtr != nil {
		customerName = *customerNamePtr
	}

	customerPhone := "Не указан"
	if customerPhonePtr != nil {
		customerPhone = *customerPhonePtr
	}

	paymentType := "Не указан"
	if paymentTypePtr != nil {
		paymentType = *paymentTypePtr
	}

	receiptStatus := "pending"
	if receiptStatusPtr != nil {
		receiptStatus = *receiptStatusPtr
	}

	statusEmoji := map[string]string{
		models.AppointmentStatusScheduled: "📅",
		models.AppointmentStatusConfirmed: "✅",
		models.AppointmentStatusCompleted: "✔️",
		models.AppointmentStatusCancelled: "❌",
		models.AppointmentStatusNoShow:    "⚠️",
	}[status]

	receiptStatusEmoji := map[string]string{
		models.ReceiptStatusPending:   "⏳",
		models.ReceiptStatusConfirmed: "✅",
		models.ReceiptStatusRejected:  "❌",
	}[receiptStatus]

	paymentEmoji := map[string]string{
		models.PaymentTypeKaspi: "💳",
		models.PaymentTypeCash:  "💵",
	}[paymentType]

	receiptInfo := ""
	if receiptURL != nil && *receiptURL != "" {
		receiptInfo = fmt.Sprintf("\n📸 Чек загружен: %s", receiptStatusEmoji)
	} else if paymentType == models.PaymentTypeKaspi {
		receiptInfo = "\n📸 Чек: ожидается"
	}

	text := fmt.Sprintf(
		"📋 ЗАПИСЬ #%d\n\n"+
			"👤 Клиент: %s\n"+
			"📞 Телефон: %s\n"+
			"💅 Услуга: %s\n"+
			"🕐 Время: %s\n"+
			"💰 Сумма: %g тг\n"+
			"💳 Оплата: %s %s\n"+
			"📊 Статус: %s %s%s\n",
		appointmentNum, customerName, customerPhone, serviceName,
		appointmentTime.Format("02.01.2006 15:04"),
		amount, paymentEmoji, paymentType,
		statusEmoji, status, receiptInfo)

	menu := &telebot.ReplyMarkup{}
	var rows []telebot.Row

	// Кнопки в зависимости от статуса
	if status == models.AppointmentStatusScheduled || status == models.AppointmentStatusConfirmed {
		rows = append(rows, menu.Row(
			menu.Data("✅ Завершить", fmt.Sprintf("apt_complete_%d", id)),
		))
	}

	if status != models.AppointmentStatusCancelled {
		rows = append(rows, menu.Row(
			menu.Data("❌ Отменить", fmt.Sprintf("apt_cancel_%d", id)),
		))
	}

	rows = append(rows, menu.Row(
		menu.Data("🏠 Назад", "admin_appointments"),
	))

	menu.Inline(rows...)

	return sendOrEdit(c, text, menu)
}
