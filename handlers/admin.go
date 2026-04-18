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
		return c.Edit("❌ Доступ запрещён. Вы не администратор.")
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
			telebot.Btn{Text: "📅 Записи", Unique: "admin_appointments"},
			telebot.Btn{Text: "💅 Услуги", Unique: "admin_services"},
		),
		menu.Row(
			telebot.Btn{Text: "⚙️ Настройки", Unique: "admin_settings"},
			telebot.Btn{Text: "📊 Статистика", Unique: "admin_stats"},
		),
	)

	return c.Send(text, menu)
}

// ========== ЗАПИСИ ==========

// HandleAdminAppointments показывает список записей
func (ah *AdminHandler) HandleAdminAppointments(c telebot.Context) error {
	ctx := context.Background()

	rows, err := ah.db.Query(ctx,
		`SELECT a.id, a.appointment_num, a.customer_name, a.status, a.appointment_time
		FROM appointments a
		WHERE a.status IN ($1, $2, $3)
		ORDER BY a.appointment_time DESC
		LIMIT 20`,
		models.AppointmentStatusScheduled, models.AppointmentStatusConfirmed, models.AppointmentStatusCompleted)
	if err != nil {
		log.Printf("❌ Ошибка получения записей: %v\n", err)
		return c.Edit("❌ Ошибка при загрузке записей")
	}
	defer rows.Close()

	menu := &telebot.ReplyMarkup{}

	for rows.Next() {
		var id, appointmentNum int
		var customerName, status string
		var appointmentTime time.Time

		if err := rows.Scan(&id, &appointmentNum, &customerName, &status, &appointmentTime); err != nil {
			log.Printf("❌ Ошибка сканирования записи: %v\n", err)
			continue
		}

		statusEmoji := map[string]string{
			models.AppointmentStatusScheduled: "📅",
			models.AppointmentStatusConfirmed: "✅",
			models.AppointmentStatusCompleted: "✔️",
			models.AppointmentStatusCancelled: "❌",
		}[status]

		menu.Inline(
			menu.Row(
				telebot.Btn{
					Text:   fmt.Sprintf("%s #%d — %s — %s", statusEmoji, appointmentNum, customerName, appointmentTime.Format("02/01 15:04")),
					Unique: fmt.Sprintf("admin_apt_detail_%d", id),
				},
			),
		)
	}

	return c.Edit("📅 Активные записи:", menu)
}

// HandleAdminServices показывает список услуг
func (ah *AdminHandler) HandleAdminServices(c telebot.Context) error {
	ctx := context.Background()

	rows, err := ah.db.Query(ctx,
		`SELECT id, name, price, duration_min, is_available FROM services ORDER BY name`)
	if err != nil {
		log.Printf("❌ Ошибка получения услуг: %v\n", err)
		return c.Edit("❌ Ошибка при загрузке услуг")
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

	return c.Edit(text, menu)
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

	salonSettings, err := ah.db.GetSalonSettings(ctx)
	if err != nil {
		log.Printf("❌ Ошибка получения настроек: %v\n", err)
		return c.Edit("❌ Ошибка")
	}

	text := fmt.Sprintf(
		"⚙️ НАСТРОЙКИ САЛОНА\n\n"+
			"Название: %s\n"+
			"Адрес: %s\n"+
			"📅 Рабочее время: %s - %s\n"+
			"🔔 Напоминание за: %d ч\n"+
			"💳 Предоплата: %d%%\n",
		salonSettings["salon_name"], salonSettings["address"],
		salonSettings["schedule_open"], salonSettings["schedule_close"],
		salonSettings["reminder_hours"], salonSettings["prepay_percent"])

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			telebot.Btn{Text: "📝 Название", Unique: "admin_set_name"},
			telebot.Btn{Text: "📍 Адрес", Unique: "admin_set_address"},
		),
		menu.Row(
			telebot.Btn{Text: "🕐 Время открытия", Unique: "admin_set_open"},
			telebot.Btn{Text: "🕐 Время закрытия", Unique: "admin_set_close"},
		),
		menu.Row(
			telebot.Btn{Text: "🔔 Напоминание", Unique: "admin_set_reminder"},
			telebot.Btn{Text: "💳 Предоплата %", Unique: "admin_set_prepay"},
		),
		menu.Row(
			telebot.Btn{Text: "💬 Поддержка ID", Unique: "admin_set_support"},
			telebot.Btn{Text: "🏠 Меню", Unique: "main_menu"},
		),
	)

	return c.Edit(text, menu)
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

// HandleAdminInputSalonName обрабатывает ввод названия
func (ah *AdminHandler) HandleAdminInputSalonName(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID
	name := strings.TrimSpace(c.Message().Text)

	if len(name) < 2 || len(name) > 100 {
		return c.Send("❌ Название должно быть от 2 до 100 символов")
	}

	// Обновляем в БД
	_, err := ah.db.Exec(ctx,
		`UPDATE salon_settings SET salon_name = $1`, name)
	if err != nil {
		log.Printf("❌ Ошибка обновления названия: %v\n", err)
		return c.Send("❌ Ошибка при сохранении")
	}

	ah.stateManager.ResetState(userID)

	text := fmt.Sprintf("✅ Название салона изменено на: %s", name)
	return c.Send(text)
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
		return c.Send("❌ Неверный формат. Используйте: 10:00")
	}

	// Обновляем в БД
	_, err := ah.db.Exec(ctx,
		`UPDATE salon_settings SET schedule_open = $1`, timeStr)
	if err != nil {
		log.Printf("❌ Ошибка обновления времени: %v\n", err)
		return c.Send("❌ Ошибка при сохранении")
	}

	ah.stateManager.ResetState(userID)

	text := fmt.Sprintf("✅ Время открытия установлено: %s", timeStr)
	return c.Send(text)
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
		return c.Send("❌ Введите число от 1 до 24")
	}

	// Обновляем в БД
	_, err = ah.db.Exec(ctx,
		`UPDATE salon_settings SET reminder_hours = $1`, hours)
	if err != nil {
		log.Printf("❌ Ошибка обновления напоминания: %v\n", err)
		return c.Send("❌ Ошибка при сохранении")
	}

	ah.stateManager.ResetState(userID)

	text := fmt.Sprintf("✅ Напоминание установлено за %d ч перед записью", hours)
	return c.Send(text)
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
		return c.Send("❌ Введите число от 0 до 100")
	}

	// Обновляем в БД
	_, err = ah.db.Exec(ctx,
		`UPDATE salon_settings SET prepay_percent = $1`, percent)
	if err != nil {
		log.Printf("❌ Ошибка обновления предоплаты: %v\n", err)
		return c.Send("❌ Ошибка при сохранении")
	}

	ah.stateManager.ResetState(userID)

	status := "отключена"
	if percent > 0 {
		status = fmt.Sprintf("установлена на %d%%", percent)
	}
	text := fmt.Sprintf("✅ Предоплата %s", status)
	return c.Send(text)
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
