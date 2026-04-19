package handlers

import (
	"context"
	"fmt"
	"io"
	"log"
	"regexp"
	"strconv"
	"time"

	"flower-bot/db"
	"flower-bot/models"
	"flower-bot/scheduler"
	"flower-bot/services"

	"gopkg.in/telebot.v3"
)

// ClientHandler обрабатывает запросы клиентов
type ClientHandler struct {
	db           *db.Database
	bot          *telebot.Bot
	stateManager *StateManager
	scheduler    *scheduler.Scheduler
	adminIDs     []int64
}

// NewClientHandler создает новый обработчик клиента
func NewClientHandler(database *db.Database, bot *telebot.Bot, sm *StateManager, sch *scheduler.Scheduler, adminIDs []int64) *ClientHandler {
	return &ClientHandler{
		db:           database,
		bot:          bot,
		stateManager: sm,
		scheduler:    sch,
		adminIDs:     adminIDs,
	}
}

// IsAdmin проверяет является ли пользователь админом
func (ch *ClientHandler) IsAdmin(userID int64) bool {
	for _, id := range ch.adminIDs {
		if id == userID {
			return true
		}
	}

	// Проверяем в БД
	ctx := context.Background()
	row := ch.db.QueryRow(ctx, "SELECT is_admin FROM users WHERE telegram_id = $1", userID)
	var isAdmin bool
	if err := row.Scan(&isAdmin); err == nil && isAdmin {
		return true
	}

	return false
}

// HandleStart обрабатывает /start
func (ch *ClientHandler) HandleStart(c telebot.Context) error {
	userID := c.Sender().ID
	ctx := context.Background()

	log.Printf("📋 [CLIENT] Пользователь (%d) нажал /start\n", userID)

	// Проверяем есть ли пользователь в БД
	row := ch.db.QueryRow(ctx, "SELECT telegram_id FROM users WHERE telegram_id = $1", userID)
	var existingID int64
	err := row.Scan(&existingID)

	if err != nil {
		// Создаем нового пользователя
		_, err := ch.db.Exec(ctx,
			`INSERT INTO users (telegram_id, username, full_name, created_at)
			VALUES ($1, $2, $3, NOW())`,
			userID, c.Sender().Username, c.Sender().FirstName+" "+c.Sender().LastName)
		if err != nil {
			log.Printf("❌ [ОШИБКА] Ошибка создания пользователя %d: %v\n", userID, err)
		} else {
			log.Printf("✅ [РЕГИСТРАЦИЯ] Пользователь %d зарегистрирован\n", userID)
		}
	}

	// Сбрасываем состояние
	ch.stateManager.ResetState(userID)

	// Очищаем старые сообщения перед новым /start
	messagesToDelete := ch.stateManager.ClearMessagesToDelete(userID)
	for _, msgID := range messagesToDelete {
		ch.bot.Delete(&telebot.Message{ID: msgID, Chat: &telebot.Chat{ID: userID}})
	}

	// Отправляем приветствие
	text := fmt.Sprintf("💅 Привет, %s!\n\nДобро пожаловать в салон красоты! 💄", c.Sender().FirstName)

	// Главное меню
	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			menu.Data("📅 Записаться", "book_appointment"),
			menu.Data("ℹ️ О нас", "about"),
		),
		menu.Row(
			menu.Data("📋 Мои записи", "my_appointments"),
			menu.Data("📞 Поддержка", "support"),
		),
	)

	log.Printf("✅ [МЕНЮ] Отправляю главное меню пользователю %d\n", userID)

	msg, err := ch.bot.Send(c.Recipient(), text, menu)
	if err == nil {
		ch.stateManager.AddMessageToDelete(userID, msg.ID)
	}

	return err
}

// HandleBookAppointment начинает процесс записи на услугу
func (ch *ClientHandler) HandleBookAppointment(c telebot.Context) error {
	userID := c.Sender().ID
	ctx := context.Background()

	log.Printf("📅 [ЗАПИСЬ] Начало процесса записи для пользователя %d\n", userID)

	// Получаем список услуг
	rows, err := ch.db.Query(ctx,
		`SELECT id, name, price, duration_min FROM services WHERE is_available = true ORDER BY name`)
	if err != nil {
		log.Printf("❌ [УСЛУГИ] Ошибка получения услуг из БД: %v\n", err)
		return c.Edit("❌ Ошибка при загрузке услуг")
	}
	defer rows.Close()

	services := []map[string]interface{}{}
	for rows.Next() {
		var id, duration int
		var name string
		var price float64
		if err := rows.Scan(&id, &name, &price, &duration); err != nil {
			log.Printf("⚠️ Ошибка сканирования услуги: %v\n", err)
			continue
		}
		log.Printf("   ✅ Услуга: %s (ID:%d, цена:%g, длит:%d мин)\n", name, id, price, duration)
		services = append(services, map[string]interface{}{
			"id":       id,
			"name":     name,
			"price":    price,
			"duration": duration,
		})
	}

	if len(services) == 0 {
		log.Printf("⚠️ [УСЛУГИ] Нет доступных услуг в базе (is_available=true)\n")
		return c.Edit("😔 К сожалению, услуги недоступны\n\nАдминистратор еще не добавил услуги. Пожалуйста, попробуйте позже.")
	}

	log.Printf("✅ [УСЛУГИ] Загружено %d услуг для пользователя %d\n", len(services), userID)

	text := "🌸 Выберите услугу:\n\n"
	for _, svc := range services {
		text += fmt.Sprintf("💅 %s • %g тг • %d мин\n",
			svc["name"], svc["price"], svc["duration"])
	}

	menu := &telebot.ReplyMarkup{}
	var btnRows []telebot.Row

	for _, svc := range services {
		btn := menu.Data(
			fmt.Sprintf("💅 %s", svc["name"]),
			fmt.Sprintf("select_service_%d", svc["id"]),
		)
		btnRows = append(btnRows, menu.Row(btn))
	}

	btnRows = append(btnRows, menu.Row(
		menu.Data("🏠 Меню", "main_menu"),
	))

	menu.Inline(btnRows...)

	msg, err := ch.bot.Send(c.Sender(), text, menu)
	if err == nil {
		ch.stateManager.AddMessageToDelete(userID, msg.ID)
	}

	return err
}

// HandleSelectService обрабатывает выбор услуги
func (ch *ClientHandler) HandleSelectService(c telebot.Context, serviceID int) error {
	userID := c.Sender().ID
	ctx := context.Background()

	log.Printf("🔍 [УСЛУГА] Пользователь %d выбрал услугу %d\n", userID, serviceID)

	// Получаем информацию об услуге
	row := ch.db.QueryRow(ctx,
		`SELECT id, name, price, duration_min FROM services WHERE id = $1`, serviceID)

	var id, duration int
	var name string
	var price float64

	if err := row.Scan(&id, &name, &price, &duration); err != nil {
		log.Printf("❌ Ошибка получения услуги: %v\n", err)
		return c.Edit("❌ Услуга не найдена")
	}

	// Сохраняем в черновик
	session := ch.stateManager.GetUserSession(userID)
	session.AppointmentDraft.ServiceID = id
	session.AppointmentDraft.Amount = price
	session.State = models.StateAwaitingDateSelection
	ch.stateManager.SetUserSession(userID, session)

	log.Printf("✅ Услуга сохранена: %s, Цена: %g\n", name, price)

	// Показываем календарь
	return ch.showDateSelection(c, userID)
}

// showDateSelection показывает календарь для выбора даты
func (ch *ClientHandler) showDateSelection(c telebot.Context, userID int64) error {
	ctx := context.Background()

	// Получаем настройки салона
	salonSettings, err := ch.db.GetSalonSettings(ctx)
	if err != nil {
		log.Printf("❌ Ошибка получения настроек салона: %v\n", err)
		return c.Edit("❌ Ошибка при загрузке расписания")
	}

	text := fmt.Sprintf("📅 Выберите дату для записи:\n\n"+
		"Рабочее время: %s - %s\n\n"+
		"Доступные даты (на 7 дней):",
		salonSettings["schedule_open"], salonSettings["schedule_close"])

	menu := &telebot.ReplyMarkup{}
	var btnRows []telebot.Row

	// Показываем 7 дней
	now := time.Now()
	days := []string{"Вс", "Пн", "Вт", "Ср", "Чт", "Пт", "Сб"}
	for i := 0; i < 7; i++ {
		date := now.AddDate(0, 0, i)
		dateStr := date.Format("2006-01-02")
		label := fmt.Sprintf("%s %s", days[date.Weekday()], date.Format("02.01"))
		btn := menu.Data(
			label,
			fmt.Sprintf("select_date_%s", dateStr),
		)
		btnRows = append(btnRows, menu.Row(btn))
	}

	btnRows = append(btnRows, menu.Row(
		menu.Data("🏠 Меню", "main_menu"),
	))

	menu.Inline(btnRows...)

	msg, err := ch.bot.Send(c.Sender(), text, menu)
	if err == nil {
		ch.stateManager.AddMessageToDelete(userID, msg.ID)
	}

	return err
}

// HandleSelectDate обрабатывает выбор даты
func (ch *ClientHandler) HandleSelectDate(c telebot.Context, dateStr string) error {
	userID := c.Sender().ID

	log.Printf("📅 [ДАТА] Пользователь %d выбрал дату %s\n", userID, dateStr)

	// Сохраняем дату
	session := ch.stateManager.GetUserSession(userID)
	session.AppointmentDraft.AppointmentDate = dateStr
	session.State = models.StateAwaitingTimeSelection
	ch.stateManager.SetUserSession(userID, session)

	// Показываем доступные времена
	return ch.showTimeSelection(c, userID, dateStr)
}

// showTimeSelection показывает доступные времена
func (ch *ClientHandler) showTimeSelection(c telebot.Context, userID int64, dateStr string) error {
	ctx := context.Background()

	// Получаем настройки салона
	salonSettings, err := ch.db.GetSalonSettings(ctx)
	if err != nil {
		log.Printf("❌ Ошибка получения настроек салона: %v\n", err)
		return c.Edit("❌ Ошибка при загрузке расписания")
	}

	scheduleOpen := salonSettings["schedule_open"].(string)
	scheduleClose := salonSettings["schedule_close"].(string)

	// Парсим время
	openHour, _ := strconv.Atoi(scheduleOpen[:2])
	closeHour, _ := strconv.Atoi(scheduleClose[:2])

	text := fmt.Sprintf("🕐 Выберите время записи на %s:\n\n"+
		"Доступное время: %s - %s",
		dateStr, scheduleOpen, scheduleClose)

	menu := &telebot.ReplyMarkup{}
	var btnRows []telebot.Row
	var allTimeBtns []telebot.Btn

	// Показываем каждый час в рабочее время
	for hour := openHour; hour < closeHour; hour++ {
		for min := 0; min < 60; min += 30 {
			timeStr := fmt.Sprintf("%02d:%02d", hour, min)
			btn := menu.Data(
				timeStr,
				fmt.Sprintf("select_time_%s_%s", dateStr, timeStr),
			)
			allTimeBtns = append(allTimeBtns, btn)
		}
	}

	// Разбиваем кнопки по 3 в ряд
	for i := 0; i < len(allTimeBtns); i += 3 {
		end := i + 3
		if end > len(allTimeBtns) {
			end = len(allTimeBtns)
		}
		btnRows = append(btnRows, menu.Row(allTimeBtns[i:end]...))
	}

	if len(btnRows) > 0 {
		menu.Inline(btnRows...)
	}

	msg, err := ch.bot.Send(c.Sender(), text, menu)
	if err == nil {
		ch.stateManager.AddMessageToDelete(userID, msg.ID)
	}

	return err
}

// HandleSelectTime обрабатывает выбор времени
func (ch *ClientHandler) HandleSelectTime(c telebot.Context, dateStr, timeStr string) error {
	userID := c.Sender().ID

	log.Printf("🕐 [ВРЕМЯ] Пользователь %d выбрал %s %s\n", userID, dateStr, timeStr)

	// Сохраняем время
	session := ch.stateManager.GetUserSession(userID)
	session.AppointmentDraft.AppointmentTime = timeStr
	session.State = models.StateAwaitingName
	ch.stateManager.SetUserSession(userID, session)

	// Запрашиваем имя
	text := "👤 Как вас зовут?"
	msg, err := ch.bot.Send(c.Sender(), text)
	if err == nil {
		ch.stateManager.AddMessageToDelete(userID, msg.ID)
	}

	return err
}

// HandleNameInput обрабатывает ввод имени
func (ch *ClientHandler) HandleNameInput(c telebot.Context) error {
	userID := c.Sender().ID
	name := c.Message().Text

	if len(name) < 2 || len(name) > 100 {
		return c.Send("❌ Имя должно быть от 2 до 100 символов")
	}

	log.Printf("👤 [ИМЯ] Пользователь %d ввел имя: %s\n", userID, name)

	// Сохраняем имя
	session := ch.stateManager.GetUserSession(userID)
	session.AppointmentDraft.Name = name
	session.FullName = name
	session.State = models.StateAwaitingPhone
	ch.stateManager.SetUserSession(userID, session)

	// Запрашиваем номер телефона
	text := "📱 Введите номер телефона (например, +77771234567) или отправьте контакт:"

	menu := &telebot.ReplyMarkup{ResizeKeyboard: true, OneTimeKeyboard: true}
	btnContact := menu.Contact("📱 Поделиться контактом")
	menu.Reply(menu.Row(btnContact))

	msg, err := ch.bot.Send(c.Sender(), text, menu)
	if err == nil {
		ch.stateManager.AddMessageToDelete(userID, msg.ID)
	}

	return err
}

// HandlePhoneInput обрабатывает ввод номера телефона
func (ch *ClientHandler) HandlePhoneInput(c telebot.Context) error {
	userID := c.Sender().ID

	var phone string
	if c.Message().Contact != nil {
		phone = c.Message().Contact.PhoneNumber
		// Убираем клавиатуру после отправки контакта
		ch.bot.Send(c.Sender(), "✅ Контакт получен", &telebot.ReplyMarkup{RemoveKeyboard: true})
	} else {
		phone = c.Message().Text
	}

	// Проверяем формат номера
	phoneRegex := regexp.MustCompile(`^\+?\d{10,15}$`)
	if !phoneRegex.MatchString(phone) {
		return c.Send("❌ Неверный формат номера. Используйте формат: +77771234567")
	}

	log.Printf("📱 [ТЕЛЕФОН] Пользователь %d ввел телефон: %s\n", userID, phone)

	// Сохраняем телефон
	session := ch.stateManager.GetUserSession(userID)
	session.AppointmentDraft.Phone = phone
	session.Phone = phone
	session.State = models.StateAwaitingPaymentMethod
	ch.stateManager.SetUserSession(userID, session)

	// Показываем выбор способа оплаты
	return ch.showPaymentMethod(c, userID)
}

// showPaymentMethod показывает выбор способа оплаты
func (ch *ClientHandler) showPaymentMethod(c telebot.Context, userID int64) error {
	ctx := context.Background()

	// Получаем настройки салона
	salonSettings, err := ch.db.GetSalonSettings(ctx)
	if err != nil {
		log.Printf("❌ Ошибка получения настроек салона: %v\n", err)
		return c.Edit("❌ Ошибка")
	}

	prepayPercent := salonSettings["prepay_percent"].(int)

	// Получаем данные записи
	session := ch.stateManager.GetUserSession(userID)
	totalAmount := session.AppointmentDraft.Amount
	prepayAmount := float64(prepayPercent) * totalAmount / 100

	text := fmt.Sprintf("💳 Способ оплаты:\n\n"+
		"Сумма услуги: %g тг\n", totalAmount)

	if prepayPercent > 0 {
		text += fmt.Sprintf("Предоплата (%d%%): %g тг\n", prepayPercent, prepayAmount)
	}

	// Проверка на защиту записи (защита от фейковых записей)
	protectionEnabled := false
	if val, ok := salonSettings["protection_enabled"].(bool); ok {
		protectionEnabled = val
	}
	protectionMinOrders := 0
	if val, ok := salonSettings["protection_min_orders"].(int); ok {
		protectionMinOrders = val
	}

	canUseCash := true
	if protectionEnabled && protectionMinOrders > 0 {
		var completedOrders int
		err := ch.db.QueryRow(ctx,
			`SELECT COUNT(*) FROM appointments WHERE user_id = $1 AND status = 'completed'`, userID).Scan(&completedOrders)
		if err == nil && completedOrders < protectionMinOrders {
			canUseCash = false
		}
	}

	menu := &telebot.ReplyMarkup{}
	var btnRows []telebot.Row

	btnRows = append(btnRows, menu.Row(
		menu.Data("💳 Перевод (Kaspi)", fmt.Sprintf("payment_kaspi_%d", userID)),
	))

	if canUseCash {
		btnRows = append(btnRows, menu.Row(
			menu.Data("💵 Наличные", fmt.Sprintf("payment_cash_%d", userID)),
		))
	} else {
		text += fmt.Sprintf("\n⚠️ Оплата наличными доступна только после %d успешно завершенных визитов.", protectionMinOrders)
	}

	btnRows = append(btnRows, menu.Row(
		menu.Data("🏠 Меню", "main_menu"),
	))

	menu.Inline(btnRows...)

	msg, err := ch.bot.Send(c.Sender(), text, menu)
	if err == nil {
		ch.stateManager.AddMessageToDelete(userID, msg.ID)
	}

	return err
}

// HandlePaymentKaspi обрабатывает оплату через Kaspi Red
func (ch *ClientHandler) HandlePaymentKaspi(c telebot.Context, userID int64) error {
	ctx := context.Background()

	log.Printf("💳 [KASPI] Пользователь %d выбрал оплату Kaspi\n", userID)

	// Получаем данные записи
	session := ch.stateManager.GetUserSession(userID)
	prepayAmount := session.AppointmentDraft.Amount

	// Получаем Kaspi ссылку из админ настроек
	salonSettings, err := ch.db.GetSalonSettings(ctx)
	if err != nil {
		log.Printf("❌ Ошибка получения настроек: %v\n", err)
		return c.Edit("❌ Ошибка")
	}

	kaspiLink := salonSettings["kaspi_link"].(string)
	if kaspiLink == "" {
		return c.Edit("❌ Система оплаты не настроена")
	}

	text := fmt.Sprintf("💳 Переводим вас на Kaspi Red для оплаты...\n\n"+
		"Сумма к оплате: %g тг\n\n"+
		"После оплаты загрузите скриншот чека ниже ⬇️",
		prepayAmount)

	// Кнопка с ссылкой на Kaspi
	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			telebot.Btn{
				Text: "💳 Оплатить через Kaspi",
				URL:  kaspiLink,
			},
		),
		menu.Row(
			telebot.Btn{Text: "✅ Я оплатил(а)", Unique: fmt.Sprintf("receipt_ready_%d", userID)},
		),
	)

	session.AppointmentDraft.PaymentType = models.PaymentTypeKaspi
	session.State = models.StateAwaitingReceipt
	ch.stateManager.SetUserSession(userID, session)

	// Расписываем дедлайн для загрузки чека (30 минут)
	ch.scheduler.ScheduleReceiptDeadline(0, userID, 30*time.Minute)

	msg, err := ch.bot.Send(c.Sender(), text, menu)
	if err == nil {
		ch.stateManager.AddMessageToDelete(userID, msg.ID)
	}

	return err
}

// HandlePaymentCash обрабатывает оплату наличными
func (ch *ClientHandler) HandlePaymentCash(c telebot.Context, userID int64) error {
	log.Printf("💵 [НАЛИЧНЫЕ] Пользователь %d выбрал оплату наличными\n", userID)

	// Сохраняем способ оплаты
	session := ch.stateManager.GetUserSession(userID)
	session.AppointmentDraft.PaymentType = models.PaymentTypeCash
	ch.stateManager.SetUserSession(userID, session)

	// Создаем запись
	return ch.createAppointment(c, userID)
}

// HandleReceiptUpload обрабатывает загрузку чека
func (ch *ClientHandler) HandleReceiptUpload(c telebot.Context) error {
	userID := c.Sender().ID

	log.Printf("📄 [ЧЕК] Пользователь %d загружает чек\n", userID)

	photo := c.Message().Photo
	if photo == nil {
		return c.Send("❌ Пожалуйста, отправьте скриншот чека")
	}

	// Получаем файл с Telegram серверов
	fileReader, err := ch.bot.File(&photo.File)
	if err != nil {
		log.Printf("❌ Ошибка получения файла: %v\n", err)
		return c.Send("❌ Ошибка при загрузке файла")
	}

	fileBytes, err := io.ReadAll(fileReader)
	if err != nil {
		log.Printf("❌ Ошибка чтения файла: %v\n", err)
		return c.Send("❌ Ошибка при чтении файла")
	}

	// Загружаем в S3
	fileName := fmt.Sprintf("receipt_%d_%d.jpg", userID, time.Now().Unix())
	receiptURL, err := services.UploadFileToS3(fileBytes, fileName, "image/jpeg", "receipts")
	if err != nil {
		log.Printf("❌ Ошибка загрузки в S3: %v\n", err)
		return c.Send("❌ Ошибка при сохранении чека")
	}

	log.Printf("✅ Чек загружен: %s\n", receiptURL)

	// Сохраняем URL чека в сессию
	session := ch.stateManager.GetUserSession(userID)
	session.TempData["receiptURL"] = receiptURL
	ch.stateManager.SetUserSession(userID, session)

	// Создаем запись
	return ch.createAppointment(c, userID)
}

// createAppointment создает запись на услугу
func (ch *ClientHandler) createAppointment(c telebot.Context, userID int64) error {
	ctx := context.Background()

	log.Printf("📅 [СОЗДАНИЕ] Создаю запись для пользователя %d\n", userID)

	session := ch.stateManager.GetUserSession(userID)
	draft := session.AppointmentDraft

	// Парсим дату и время
	dateTimeStr := draft.AppointmentDate + " " + draft.AppointmentTime
	appointmentTime, err := time.Parse("2006-01-02 15:04", dateTimeStr)
	if err != nil {
		log.Printf("❌ Ошибка парсинга даты/времени: %v\n", err)
		return c.Send("❌ Ошибка при обработке даты")
	}

	// Получаем информацию об услуге
	row := ch.db.QueryRow(ctx,
		`SELECT name, price FROM services WHERE id = $1`, draft.ServiceID)
	var serviceName string
	var price float64
	if err := row.Scan(&serviceName, &price); err != nil {
		log.Printf("❌ Ошибка получения услуги: %v\n", err)
		return c.Send("❌ Услуга не найдена")
	}

	// Определяем URL чека (если есть)
	receiptURL := ""
	if url, ok := session.TempData["receiptURL"].(string); ok {
		receiptURL = url
	}

	// Создаем запись
	receiptStatus := ""
	if draft.PaymentType == models.PaymentTypeKaspi && receiptURL != "" {
		receiptStatus = models.ReceiptStatusPending // Чек ожидает подтверждения
	}

	insertRow := ch.db.QueryRow(ctx,
		`INSERT INTO appointments (
			user_id, service_id, appointment_time, payment_type, amount, 
			customer_name, customer_phone, status, receipt_url, receipt_status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
		RETURNING id, appointment_num`,
		userID, draft.ServiceID, appointmentTime, draft.PaymentType, price,
		draft.Name, draft.Phone, models.AppointmentStatusScheduled,
		nullableString(receiptURL), nullableString(receiptStatus))

	var appointmentID, appointmentNum int
	if err := insertRow.Scan(&appointmentID, &appointmentNum); err != nil {
		log.Printf("❌ Ошибка создания записи: %v\n", err)
		return c.Send("❌ Ошибка при создании записи")
	}

	log.Printf("✅ Запись создана: ID=%d, Номер=%d\n", appointmentID, appointmentNum)

	// Если оплата Kaspi и чек не загружен - расписываем напоминание про чек
	if draft.PaymentType == models.PaymentTypeKaspi && receiptURL == "" {
		ch.scheduler.ScheduleReceiptDeadline(appointmentID, userID, 30*time.Minute)
	}

	// Расписываем напоминалку перед записью
	ch.scheduler.ScheduleAppointmentReminder(appointmentID, userID, appointmentTime)

	// Сбрасываем состояние
	ch.stateManager.ResetState(userID)

	// Отправляем подтверждение
	text := fmt.Sprintf("✅ Запись создана!\n\n"+
		"📋 Номер записи: #%d\n"+
		"💅 Услуга: %s\n"+
		"📅 Дата: %s\n"+
		"🕐 Время: %s\n"+
		"💰 Сумма: %g тг\n"+
		"💳 Оплата: %s\n",
		appointmentNum, serviceName, draft.AppointmentDate, draft.AppointmentTime,
		price, getDraftPaymentTypeText(draft.PaymentType))

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			telebot.Btn{Text: "📋 Мои записи", Unique: "my_appointments"},
			telebot.Btn{Text: "🏠 Меню", Unique: "main_menu"},
		),
	)

	msg, err := ch.bot.Send(c.Sender(), text, menu)
	if err == nil {
		ch.stateManager.AddMessageToDelete(userID, msg.ID)
	}

	// Отправляем админу уведомление о новой записи
	ch.notifyAdminNewAppointment(ctx, appointmentID, appointmentNum, draft.Name, serviceName, draft.AppointmentDate, draft.AppointmentTime, receiptURL != "")

	return err
}

// HandleMyAppointments показывает записи пользователя
func (ch *ClientHandler) HandleMyAppointments(c telebot.Context) error {
	userID := c.Sender().ID
	ctx := context.Background()

	log.Printf("📋 [ЗАПИСИ] Пользователь %d запросил свои записи\n", userID)

	rows, err := ch.db.Query(ctx,
		`SELECT a.id, a.appointment_num, s.name, a.appointment_time, a.status, a.amount
		FROM appointments a
		JOIN services s ON a.service_id = s.id
		WHERE a.user_id = $1 AND a.status != $2
		ORDER BY a.appointment_time DESC
		LIMIT 10`,
		userID, models.AppointmentStatusCancelled)
	if err != nil {
		log.Printf("❌ Ошибка получения записей: %v\n", err)
		return c.Send("❌ Ошибка при загрузке записей")
	}
	defer rows.Close()

	text := "📋 Ваши записи:\n\n"
	hasAppointments := false

	for rows.Next() {
		var id, appointmentNum int
		var serviceName, status string
		var appointmentTime time.Time
		var amount float64

		if err := rows.Scan(&id, &appointmentNum, &serviceName, &appointmentTime, &status, &amount); err != nil {
			log.Printf("⚠️ Ошибка сканирования записи: %v\n", err)
			continue
		}

		hasAppointments = true
		statusEmoji := map[string]string{
			models.AppointmentStatusScheduled: "📅",
			models.AppointmentStatusConfirmed: "✅",
			models.AppointmentStatusCompleted: "✔️",
			models.AppointmentStatusCancelled: "❌",
		}[status]

		text += fmt.Sprintf("%s #%d • %s • %s • %g тг\n",
			statusEmoji, appointmentNum, serviceName,
			appointmentTime.Format("02/01 15:04"), amount)
	}

	if !hasAppointments {
		text = "📋 У вас нет активных записей\n"
	}

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			telebot.Btn{Text: "📅 Записаться", Unique: "book_appointment"},
			telebot.Btn{Text: "🏠 Меню", Unique: "main_menu"},
		),
	)

	msg, err := ch.bot.Send(c.Sender(), text, menu)
	if err == nil {
		ch.stateManager.AddMessageToDelete(userID, msg.ID)
	}

	return err
}

// notifyAdminNewAppointment отправляет уведомление администратору
func (ch *ClientHandler) notifyAdminNewAppointment(ctx context.Context, appointmentID, appointmentNum int, customerName, serviceName, date, time string, hasReceipt bool) {
	// Получаем ID поддержки
	salonSettings, err := ch.db.GetSalonSettings(ctx)
	if err != nil || salonSettings["support_user_id"] == nil {
		log.Printf("⚠️ Поддержка не настроена\n")
		return
	}

	supportUserID := salonSettings["support_user_id"].(*int64)
	if supportUserID == nil {
		return
	}

	text := fmt.Sprintf("🔔 Новая запись!\n\n"+
		"#%d\n"+
		"Клиент: %s\n"+
		"Услуга: %s\n"+
		"Дата: %s\n"+
		"Время: %s",
		appointmentNum, customerName, serviceName, date, time)

	// Если есть чек на подтверждение
	if hasReceipt {
		text += "\n\n📄 Требуется подтверждение чека!"
	}

	menu := &telebot.ReplyMarkup{}

	// Если есть чек - добавляем кнопку подтверждения
	if hasReceipt {
		menu.Inline(
			menu.Row(
				telebot.Btn{
					Text:   "✅ Подтвердить чек",
					Unique: fmt.Sprintf("confirm_receipt_%d", appointmentID),
				},
				telebot.Btn{
					Text:   "❌ Отклонить",
					Unique: fmt.Sprintf("reject_receipt_%d", appointmentID),
				},
			),
		)
	}

	user := &telebot.User{ID: *supportUserID}
	if _, err := ch.bot.Send(user, text, menu); err != nil {
		log.Printf("⚠️ Ошибка отправки уведомления админу: %v\n", err)
	}
}

// Helper function
func getDraftPaymentTypeText(paymentType string) string {
	if paymentType == models.PaymentTypeKaspi {
		return "💳 Kaspi Red"
	}
	return "💵 Наличные"
}

// Helper function for nullable strings
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
