package handlers

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"flower-bot/db"
	"flower-bot/models"
	"flower-bot/scheduler"

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
		// bot.Delete expects a message struct with an ID and a Chat containing the chat ID
		ch.bot.Delete(&telebot.Message{ID: msgID, Chat: &telebot.Chat{ID: userID}})
	}

	// Отправляем приветствие
	text := fmt.Sprintf("🌸 Привет, %s!\n\nДобро пожаловать в магазин цветов! 🌹", c.Sender().FirstName)

	// Главное меню с внутристрочными кнопками
	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			telebot.Btn{Text: "🌸 Каталог", Unique: "catalog"},
			telebot.Btn{Text: "ℹ️ О нас", Unique: "about"},
		),
		menu.Row(
			telebot.Btn{Text: "📦 Мои заказы", Unique: "my_orders"},
			telebot.Btn{Text: "🎨 Свой букет", Unique: "custom_bouquet"},
		),
		menu.Row(
			telebot.Btn{Text: "📍 Адрес", Unique: "address"},
			telebot.Btn{Text: "💬 Поддержка", Unique: "support"},
		),
	)

	log.Printf("✅ [МЕНЮ] Отправляю главное меню пользователю %d\n", userID)

	msg, err := ch.bot.Send(c.Recipient(), text, menu)
	if err == nil {
		ch.stateManager.AddMessageToDelete(userID, msg.ID)
	}

	return err
}

// HandleCatalog показывает каталог букетов с inline кнопками (с пагинацией)
func (ch *ClientHandler) HandleCatalog(c telebot.Context) error {
	return ch.HandleCatalogPage(c, 0)
}

// HandleCatalogPage показывает конкретную страницу каталога
func (ch *ClientHandler) HandleCatalogPage(c telebot.Context, page int) error {
	userID := c.Sender().ID
	ctx := context.Background()

	log.Printf("\n📦 [КАТАЛОГ] === ОТКРЫТИЕ КАТАЛОГА (страница %d) ===\n", page+1)
	log.Printf("   Пользователь: %d (@%s)\n", userID, c.Sender().Username)

	// Получаем букеты из БД
	log.Printf("   🔍 [БД] Запрашиваю букеты...\n")
	rows, err := ch.db.Query(ctx,
		`SELECT id, name, price, photo_urls, quantity, is_available, reserved_until, reserved_by
		FROM bouquets ORDER BY created_at DESC`)
	if err != nil {
		log.Printf("   ❌ [БД] Ошибка получения букетов: %v\n", err)
		return c.Edit("❌ Ошибка при загрузке каталога")
	}
	defer rows.Close()

	bouquets := []*models.Bouquet{}
	for rows.Next() {
		var b models.Bouquet
		if err := rows.Scan(&b.ID, &b.Name, &b.Price, &b.PhotoURLs, &b.Quantity, &b.IsAvailable, &b.ReservedUntil, &b.ReservedBy); err != nil {
			log.Printf("   ⚠️ [БД] Ошибка сканирования букета: %v\n", err)
			continue
		}
		bouquets = append(bouquets, &b)
		log.Printf("   ✅ Загружен букет: ID=%d, Name='%s', Price=%g, Qty=%d\n", b.ID, b.Name, b.Price, b.Quantity)
	}

	if len(bouquets) == 0 {
		log.Printf("   ⚠️ [КАТАЛОГ] Каталог пуст\n")
		return c.Edit("😔 Каталог пусто")
	}

	log.Printf("   📊 Всего букетов: %d\n", len(bouquets))

	// Пагинация: 5 букетов на странице
	const pageSize = 5
	totalPages := (len(bouquets) + pageSize - 1) / pageSize

	if page >= totalPages {
		page = totalPages - 1
	}
	if page < 0 {
		page = 0
	}

	start := page * pageSize
	end := start + pageSize
	if end > len(bouquets) {
		end = len(bouquets)
	}

	pageBouquets := bouquets[start:end]
	log.Printf("   📄 Страница %d/%d | Букеты %d-%d\n", page+1, totalPages, start+1, end)

	// Создаем текст с информацией
	text := fmt.Sprintf("🌸 НАШИ БУКЕТЫ (страница %d/%d)\n\n", page+1, totalPages)
	for _, b := range pageBouquets {
		available := ""
		if b.Quantity <= 0 {
			available = " ❌"
		} else if b.Quantity <= 3 {
			available = fmt.Sprintf(" ⚠️ ×%d", b.Quantity)
		}
		text += fmt.Sprintf("🌹 %s • %g тг%s\n", b.Name, b.Price, available)
	}

	menu := &telebot.ReplyMarkup{}
	var btnRows []telebot.Row

	// Кнопки для каждого букета на странице
	for _, b := range pageBouquets {
		isReservedByOther := b.ReservedUntil != nil && b.ReservedUntil.After(time.Now()) &&
			(b.ReservedBy == nil || *b.ReservedBy != userID)

		var btn telebot.Btn
		if isReservedByOther {
			btn = menu.Data(fmt.Sprintf("⏳ %s", b.Name), fmt.Sprintf("reserved_%d", b.ID))
		} else if b.Quantity <= 0 {
			btn = menu.Data(fmt.Sprintf("❌ %s", b.Name), fmt.Sprintf("unavail_%d", b.ID))
		} else {
			btn = menu.Data(fmt.Sprintf("🌹 %s", b.Name), fmt.Sprintf("order_%d", b.ID))
		}
		btnRows = append(btnRows, menu.Row(btn))
	}

	// Кнопки навигации
	navRow := []telebot.Btn{}
	if page > 0 {
		navRow = append(navRow, menu.Data("◀️ Назад", fmt.Sprintf("catalog_page_%d", page-1)))
	}
	if page < totalPages-1 {
		navRow = append(navRow, menu.Data("Вперед ▶️", fmt.Sprintf("catalog_page_%d", page+1)))
	}
	navRow = append(navRow, menu.Data("🏠 Меню", "main_menu"))

	btnRows = append(btnRows, menu.Row(navRow...))
	menu.Inline(btnRows...)
	log.Printf("   📤 Отправляю каталог (страница %d)...\n", page+1)

	// Ответим на callback если это callback
	if c.Query() != nil {
		c.Respond()
	}

	// Отправляем новое сообщение с каталогом
	msg, err := ch.bot.Send(c.Sender(), text, menu)
if err == nil {
ch.stateManager.AddMessageToDelete(userID, msg.ID)
}
if err != nil {
		log.Printf("   ❌ [ОШИБКА] Ошибка отправки каталога: %v\n", err)
		return err
	}

	log.Printf("   ✅ [КАТАЛОГ] Каталог отправлен успешно\n\n")
	return nil
}

// HandleOrderStart начинает процесс заказа букета
func (ch *ClientHandler) HandleOrderStart(c telebot.Context, bouquetID int) error {
	ctx := context.Background()
	userID := c.Sender().ID

	log.Printf("\n🛒 [ЗАКАЗ] === НАЧАЛО ПРОЦЕССА ЗАКАЗА ===\n")
	log.Printf("   Пользователь: %d | BouquetID: %d\n", userID, bouquetID)

	// Проверяем антиспам
	log.Printf("   🔍 [ПРОВЕРКА] Проверяю антиспам...\n")
	row := ch.db.QueryRow(ctx,
		`SELECT last_order_attempt FROM users WHERE telegram_id = $1`, userID)
	var lastAttempt *time.Time
	if err := row.Scan(&lastAttempt); err != nil {
		log.Printf("❌ Ошибка получения последней попытки заказа: %v\n", err)
	}

	if lastAttempt != nil {
		elapsed := time.Since(*lastAttempt)
		if elapsed < 30*time.Minute {
			remaining := (30 * time.Minute) - elapsed
			log.Printf("   ❌ [АНТИСПАМ] Попытка слишком часто. Осталось: %d мин\n", int(remaining.Minutes()))
			return c.Edit(fmt.Sprintf("⏳ Подождите перед новым заказом. Осталось: %d мин", int(remaining.Minutes())))
		}
	}
	log.Printf("   ✅ [АНТИСПАМ] Проверка пройдена\n")

	// Получаем букет
	log.Printf("   🔍 [БД] Запрашиваю букет ID=%d...\n", bouquetID)
	row = ch.db.QueryRow(ctx,
		`SELECT id, name, description, price, photo_urls, quantity, reserved_until, reserved_by
		FROM bouquets WHERE id = $1`, bouquetID)

	var bouquet models.Bouquet
	if err := row.Scan(&bouquet.ID, &bouquet.Name, &bouquet.Description, &bouquet.Price, &bouquet.PhotoURLs, &bouquet.Quantity, &bouquet.ReservedUntil, &bouquet.ReservedBy); err != nil {
		log.Printf("   ❌ [БД] Ошибка получения букета: %v\n", err)
		return c.Edit("❌ Букет не найден")
	}
	log.Printf("   ✅ [БД] Букет найден: '%s', Цена: %g, В наличии: %d\n", bouquet.Name, bouquet.Price, bouquet.Quantity)

	// Проверяем доступность
	log.Printf("   🔍 [ПРОВЕРКА] Проверяю доступность букета...\n")
	if bouquet.Quantity <= 0 {
		log.Printf("   ❌ Букет нет в наличии\n")
		return c.Edit("❌ Букет нет в наличии")
	}

	// Проверяем резерв - блокируем только если зарезервирован ДРУГИМ пользователем
	isReservedByOther := bouquet.ReservedUntil != nil && bouquet.ReservedUntil.After(time.Now()) &&
		(bouquet.ReservedBy == nil || *bouquet.ReservedBy != userID)
	if isReservedByOther {
		log.Printf("   ❌ Букет зарезервирован другим пользователем до: %v\n", bouquet.ReservedUntil)
		return c.Edit("⏳ Букет уже зарезервирован другим пользователем")
	}
	log.Printf("   ✅ [ДОСТУПНОСТЬ] Букет доступен\n")

	// Показываем подробности букета
	log.Printf("   📤 [ДЕТАЛИ] Показываю подробности букета...\n")

	caption := fmt.Sprintf("🌸 %s\n\n📝 %s\n\n💰 Цена: %g тг",
		bouquet.Name, bouquet.Description, bouquet.Price)

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(telebot.Btn{Text: "🛒 Заказать", Unique: fmt.Sprintf("orderconfirm_%d", bouquetID)}),
		menu.Row(telebot.Btn{Text: "◀️ Назад в каталог", Unique: "back_to_catalog"}),
	)

	// Если есть валидное фото, отправляем с фото

	if len(bouquet.PhotoURLs) > 1 {
		log.Printf("   📸 [АЛЬБОМ] Отправляю фото альбомом\n")
		var album telebot.Album
		for i, pURL := range bouquet.PhotoURLs {
			pURL = strings.TrimSpace(pURL)
			if pURL != "" && pURL != "NULL" && strings.HasPrefix(pURL, "http") {
				photo := &telebot.Photo{File: telebot.FromURL(pURL)}
				if i == 0 {
					photo.Caption = caption
				}
				album = append(album, photo)
			}
		}

		if len(album) > 0 {
			ch.bot.SendAlbum(c.Sender(), album)
			// Альбом не поддерживает кнопки, поэтому отправляем отдельным сообщением
			if _, err := ch.bot.Send(c.Sender(), "Выберите действие:", menu); err != nil {
				return err
			}
			return nil
		}
	}

	var photoURL string
	if len(bouquet.PhotoURLs) > 0 {
		photoURL = strings.TrimSpace(bouquet.PhotoURLs[0])
	}

	if photoURL != "" && photoURL != "NULL" && strings.HasPrefix(photoURL, "http") {
		log.Printf("   📸 [ФОТО] Отправляю с фото\n")
		photo := &telebot.Photo{File: telebot.FromURL(photoURL)}
		if _, err := ch.bot.Send(c.Sender(), photo, caption, menu); err != nil {
			log.Printf("   ⚠️ [ФОТО] Ошибка отправки с фото, пробую без фото\n")
			if _, err := ch.bot.Send(c.Sender(), caption, menu); err != nil {
				log.Printf("   ❌ [ОШИБКА] Ошибка показа деталей: %v\n", err)
				return err
			}
		}
	} else {
		log.Printf("   ⚠️ [ФОТО] Фото недоступно, отправляю только текст\n")
		if _, err := ch.bot.Send(c.Sender(), caption, menu); err != nil {
			log.Printf("   ❌ [ОШИБКА] Ошибка показа деталей: %v\n", err)
			return err
		}
	}

	log.Printf("   ✅ [ДЕТАЛИ] Подробности показаны\n")
	return nil
}

// HandleOrderConfirmFromDetails подтверждает заказ после просмотра деталей
func (ch *ClientHandler) HandleOrderConfirmFromDetails(c telebot.Context, bouquetID int) error {
	ctx := context.Background()
	userID := c.Sender().ID

	log.Printf("\n💳 [ЗАКАЗ] === ПОДТВЕРЖДЕНИЕ ЗАКАЗА ===\n")
	log.Printf("   Пользователь: %d | BouquetID: %d\n", userID, bouquetID)

	// Получаем букет для проверки
	log.Printf("   🔍 [БД] Проверяю доступность букета...\n")
	row := ch.db.QueryRow(ctx,
		`SELECT id, name, price, quantity, reserved_until, reserved_by 
		FROM bouquets WHERE id = $1`, bouquetID)

	var bouquet models.Bouquet
	if err := row.Scan(&bouquet.ID, &bouquet.Name, &bouquet.Price, &bouquet.Quantity, &bouquet.ReservedUntil, &bouquet.ReservedBy); err != nil {
		log.Printf("   ❌ [БД] Ошибка получения букета: %v\n", err)
		return c.Edit("❌ Букет не найден")
	}

	// Проверяем доступность
	if bouquet.Quantity <= 0 {
		log.Printf("   ❌ Букет нет в наличии\n")
		return c.Edit("❌ Букет нет в наличии")
	}

	// Проверяем резерв - блокируем только если зарезервирован ДРУГИМ пользователем
	isReservedByOther := bouquet.ReservedUntil != nil && bouquet.ReservedUntil.After(time.Now()) &&
		(bouquet.ReservedBy == nil || *bouquet.ReservedBy != userID)
	if isReservedByOther {
		log.Printf("   ❌ Букет зарезервирован другим пользователем\n")
		return c.Edit("⏳ Букет уже зарезервирован другим пользователем")
	}

	// Резервируем букет
	log.Printf("   💾 [БД] Резервирую букет на 30 минут...\n")
	_, err := ch.db.Exec(ctx,
		`UPDATE bouquets SET reserved_by = $1, reserved_until = NOW() + INTERVAL '30 minutes'
		WHERE id = $2`, userID, bouquetID)
	if err != nil {
		log.Printf("   ❌ Ошибка резервирования букета: %v\n", err)
		return c.Edit("❌ Ошибка при резервировании букета")
	}
	log.Printf("   ✅ [БД] Букет зарезервирован\n")

	// Расписываем таймер на снятие резерва
	ch.scheduler.ScheduleReservationExpiry(bouquetID, userID, 30*time.Minute)

	// Сохраняем черновик заказа
	draft := models.OrderDraft{
		BouquetID: bouquetID,
		Amount:    bouquet.Price,
	}
	ch.stateManager.SetOrderDraft(userID, draft)

	// Переводим в состояние выбора типа доставки
	ch.stateManager.SetState(userID, models.StateNone)

	log.Printf("   ✅ [ЗАКАЗ] Начинаю процесс заказа - спрашиваю доставку\n")
	return ch.HandleDeliverySelect(c)
}

// HandleNameInput обрабатывает ввод имени
func (ch *ClientHandler) HandleNameInput(c telebot.Context) error {
	userID := c.Sender().ID
	text := c.Message().Text

	if text == "" || len(text) < 2 {
		return c.Send("❌ Пожалуйста, введите корректное имя")
	}

	// Сохраняем имя
	draft := ch.stateManager.GetOrderDraft(userID)
	if draft != nil {
		draft.Name = text
		ch.stateManager.SetOrderDraft(userID, *draft)
	}

	// Переходим к телефону
	ch.stateManager.SetState(userID, models.StateAwaitingPhone)

	menu := &telebot.ReplyMarkup{ForceReply: true}
	return c.Send("📱 Ваш номер телефона? (например: +77001234567)", menu)
}

// HandleDeliverySelect спрашивает тип доставки
func (ch *ClientHandler) HandleDeliverySelect(c telebot.Context) error {
	userID := c.Sender().ID

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			telebot.Btn{Text: "🚚 Доставка", Unique: fmt.Sprintf("delivery_choice_%d_delivery", userID)},
			telebot.Btn{Text: "🏪 Самовывоз", Unique: fmt.Sprintf("delivery_choice_%d_pickup", userID)},
		),
	)

	caption := "🚚 Выберите способ получения букета:"
	if c.Message() != nil {
		return c.Send(caption, menu)
	}
	return c.Edit(caption, menu)
}

// HandleDeliveryChoiceSelect обрабатывает выбор типа доставки и спрашивает дату
func (ch *ClientHandler) HandleDeliveryChoiceSelect(c telebot.Context, deliveryType string) error {
	userID := c.Sender().ID

	// Сохраняем тип доставки
	draft := ch.stateManager.GetOrderDraft(userID)
	if draft != nil {
		draft.DeliveryType = deliveryType
		ch.stateManager.SetOrderDraft(userID, *draft)
	}

	// Спрашиваем дату доставки
	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			telebot.Btn{Text: "🕐 Сегодня", Unique: fmt.Sprintf("delivery_date_%d_today", userID)},
			telebot.Btn{Text: "📅 Другую дату", Unique: fmt.Sprintf("delivery_date_%d_other", userID)},
		),
	)

	log.Printf("📅 [ЗАКАЗ] Тип доставки выбран: %s, спрашиваю дату\n", deliveryType)
	return c.Edit("📅 Когда нужен букет?", menu)
}

// HandleDeliveryDateSelect обрабатывает выбор даты доставки
func (ch *ClientHandler) HandleDeliveryDateSelect(c telebot.Context, dateChoice string) error {
	userID := c.Sender().ID

	// Сохраняем дату
	draft := ch.stateManager.GetOrderDraft(userID)
	if draft == nil {
		return c.Send("❌ Ошибка заказа. Пожалуйста, начните заново")
	}

	if dateChoice == "today" {
		// Для "сегодня" сохраняем текущую дату
		draft.DeliveryDate = time.Now().Format("2006-01-02")
		ch.stateManager.SetOrderDraft(userID, *draft)

		// Переходим к имени
		ch.stateManager.SetState(userID, models.StateAwaitingName)
		menu := &telebot.ReplyMarkup{ForceReply: true}
		log.Printf("📅 [ЗАКАЗ] Дата выбрана: СЕГОДНЯ (%s), спрашиваю имя\n", draft.DeliveryDate)
		return c.Send("👤 Как вас зовут?", menu)
	} else if dateChoice == "other" {
		// Для "другую дату" спросим ввод
		ch.stateManager.SetState(userID, models.StateAwaitingDate)
		menu := &telebot.ReplyMarkup{ForceReply: true}
		log.Printf("📅 [ЗАКАЗ] Запрашиваю дату доставки\n")
		return c.Send("📅 Введите дату доставки (ДД.ММ.ГГГГ):", menu)
	}

	return c.Send("❌ Ошибка при выборе даты")
}

// HandlePhoneInput обрабатывает ввод телефона
func (ch *ClientHandler) HandlePhoneInput(c telebot.Context) error {
	userID := c.Sender().ID
	text := c.Message().Text

	// Валидируем телефон - только цифры, 10-11 символов
	phoneRegex := regexp.MustCompile(`^[\d+\-\s\(\)]{10,}$`)
	if !phoneRegex.MatchString(text) {
		return c.Send("❌ Пожалуйста, введите корректный номер телефона")
	}

	// Очищаем от спецсимволов
	phone := regexp.MustCompile(`[^\d+]`).ReplaceAllString(text, "")

	// Сохраняем телефон
	draft := ch.stateManager.GetOrderDraft(userID)
	if draft != nil {
		draft.Phone = phone
		ch.stateManager.SetOrderDraft(userID, *draft)
	}

	// Проверяем тип доставки и переходим к адресу или оплате
	if draft != nil && draft.DeliveryType == models.DeliveryTypeDelivery {
		// Для доставки просим адрес
		ch.stateManager.SetState(userID, models.StateAwaitingAddress)
		menu := &telebot.ReplyMarkup{ForceReply: true}
		log.Printf("📬 [ЗАКАЗ] Спрашиваю адрес доставки\n")
		return c.Send("🏠 Введите адрес доставки:", menu)
	} else {
		// Для самовывоза сразу к оплате
		ch.stateManager.SetState(userID, models.StateNone)
		menu := &telebot.ReplyMarkup{}
		menu.Inline(
			menu.Row(
				telebot.Btn{Text: "💳 Kaspi Pay", Unique: fmt.Sprintf("payment_type_%d_kaspi", userID)},
				telebot.Btn{Text: "💵 Наличные", Unique: fmt.Sprintf("payment_type_%d_cash", userID)},
			),
		)
		log.Printf("💳 [ЗАКАЗ] Переходу к оплате (самовывоз)\n")
		return c.Send("💳 Выберите способ оплаты:", menu)
	}
}

// HandleDateInput обрабатывает ввод даты доставки
func (ch *ClientHandler) HandleDateInput(c telebot.Context) error {
	userID := c.Sender().ID
	text := strings.TrimSpace(c.Message().Text)

	// Валидируем формат даты (ДД.ММ.ГГГГ или ГГГГ-ММ-ДД)
	dateRegex := regexp.MustCompile(`^(\d{2}\.\d{2}\.\d{4}|\d{4}-\d{2}-\d{2})$`)
	if !dateRegex.MatchString(text) {
		return c.Send("❌ Пожалуйста, введите дату в формате ДД.ММ.ГГГГ (например: 25.12.2024)")
	}

	// Нормализуем формат даты
	var dateFormatted string
	if strings.Contains(text, ".") {
		// ДД.ММ.ГГГГ -> ГГГГ-ММ-ДД
		parts := strings.Split(text, ".")
		dateFormatted = fmt.Sprintf("%s-%s-%s", parts[2], parts[1], parts[0])
	} else {
		dateFormatted = text
	}

	// Проверяем что дата не в прошлом
	parsedDate, err := time.Parse("2006-01-02", dateFormatted)
	if err != nil {
		return c.Send("❌ Неверная дата. Пожалуйста, введите корректную дату")
	}

	today := time.Now()
	if parsedDate.Before(today.Add(-24 * time.Hour)) {
		return c.Send("❌ Дата не может быть в прошлом. Пожалуйста, выберите будущую дату")
	}

	// Сохраняем дату
	draft := ch.stateManager.GetOrderDraft(userID)
	if draft != nil {
		draft.DeliveryDate = dateFormatted
		ch.stateManager.SetOrderDraft(userID, *draft)
	}

	// Переходим к имени
	ch.stateManager.SetState(userID, models.StateAwaitingName)
	menu := &telebot.ReplyMarkup{ForceReply: true}
	log.Printf("📅 [ЗАКАЗ] Дата установлена: %s, спрашиваю имя\n", dateFormatted)
	return c.Send("👤 Как вас зовут?", menu)
}

// HandleDeliveryTypeSelect обрабатывает выбор типа доставки
func (ch *ClientHandler) HandleDeliveryTypeSelect(c telebot.Context, deliveryType string) error {
	userID := c.Sender().ID

	// Сохраняем тип доставки
	draft := ch.stateManager.GetOrderDraft(userID)
	if draft != nil {
		draft.DeliveryType = deliveryType
		ch.stateManager.SetOrderDraft(userID, *draft)
	}

	reply := &telebot.ReplyMarkup{}

	if deliveryType == models.DeliveryTypeDelivery {
		// Для доставки просим адрес
		ch.stateManager.SetState(userID, models.StateAwaitingAddress)
		reply.ForceReply = true
		return c.Send("🏠 Введите адрес доставки:", reply)
	} else {
		// Для самовывоза сразу к оплате
		ch.stateManager.SetState(userID, models.StateNone)
		menu := &telebot.ReplyMarkup{}
		menu.Inline(
			menu.Row(
				telebot.Btn{Text: "💳 Kaspi Pay", Unique: fmt.Sprintf("payment_type_%d_kaspi", userID)},
				telebot.Btn{Text: "💵 Наличные", Unique: fmt.Sprintf("payment_type_%d_cash", userID)},
			),
		)
		return c.Edit("💳 Выберите способ оплаты:", menu)
	}
}

// HandleAddressInput обрабатывает ввод адреса
func (ch *ClientHandler) HandleAddressInput(c telebot.Context) error {
	userID := c.Sender().ID
	text := c.Message().Text

	if text == "" || len(text) < 5 {
		return c.Send("❌ Пожалуйста, введите полный адрес")
	}

	// Сохраняем адрес
	draft := ch.stateManager.GetOrderDraft(userID)
	if draft != nil {
		draft.Address = text
		ch.stateManager.SetOrderDraft(userID, *draft)
	}

	// Переходим к оплате (для доставки только Kaspi)
	ch.stateManager.SetState(userID, models.StateNone)

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			telebot.Btn{Text: "💳 Kaspi Pay", Unique: fmt.Sprintf("payment_type_%d_kaspi", userID)},
		),
	)
	return c.Send("💳 Для доставки доступна только оплата через Kaspi Pay:", menu)
}

// HandlePaymentTypeSelect обрабатывает выбор способа оплаты
func (ch *ClientHandler) HandlePaymentTypeSelect(c telebot.Context, paymentType string) error {
	ctx := context.Background()
	userID := c.Sender().ID

	// Сохраняем тип оплаты
	draft := ch.stateManager.GetOrderDraft(userID)
	if draft != nil {
		draft.PaymentType = paymentType
		ch.stateManager.SetOrderDraft(userID, *draft)
	}

	// Получаем информацию о букете
	row := ch.db.QueryRow(ctx,
		`SELECT name FROM bouquets WHERE id = $1`, draft.BouquetID)
	var bouquetName string
	if err := row.Scan(&bouquetName); err != nil {
		log.Printf("❌ Ошибка получения названия букета: %v\n", err)
		return c.Edit("❌ Ошибка при получении информации о букете")
	}

	// Формируем итоговое сообщение
	summary := fmt.Sprintf("📋 <b>Ваш заказ:</b>\n"+
		"🌸 Букет: <b>%s</b>\n"+
		"📦 Получение: %s\n"+
		"� Дата: %s\n"+
		"👤 Имя: %s\n"+
		"📱 Телефон: %s\n"+
		"💳 Оплата: %s\n",
		bouquetName,
		map[string]string{models.DeliveryTypeDelivery: "🚚 Доставка", models.DeliveryTypePickup: "🏪 Самовывоз"}[draft.DeliveryType],
		draft.DeliveryDate,
		draft.Name,
		draft.Phone,
		map[string]string{models.PaymentTypeKaspi: "💳 Kaspi Pay", models.PaymentTypeCash: "💵 Наличные"}[draft.PaymentType])

	if draft.DeliveryType == models.DeliveryTypeDelivery {
		summary += fmt.Sprintf("📍 Адрес: <b>%s</b>\n", draft.Address)
	}

	summary += fmt.Sprintf("💰 Сумма: <b>%g тг</b>\n", draft.Amount)

	if draft.PaymentType == models.PaymentTypeKaspi {
		if draft.DeliveryType == models.DeliveryTypePickup {
			summary += fmt.Sprintf("⏰ К оплате сейчас (50%%): <b>%g тг</b>\n", draft.Amount*0.5)
		} else {
			summary += fmt.Sprintf("⏰ К оплате сейчас (100%%): <b>%g тг</b>\n", draft.Amount)
		}
	}

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			telebot.Btn{Text: "✅ Подтвердить", Unique: fmt.Sprintf("confirm_order_%d", userID)},
			telebot.Btn{Text: "❌ Отмена", Unique: fmt.Sprintf("cancel_order_%d", userID)},
		),
	)

	return c.Edit(summary, &telebot.SendOptions{ParseMode: telebot.ModeHTML}, menu)
}

// HandleConfirmOrder подтверждает заказ
func (ch *ClientHandler) HandleConfirmOrder(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID

	draft := ch.stateManager.GetOrderDraft(userID)
	if draft == nil {
		return c.Edit("❌ Ошибка: заказ не найден")
	}

	// Обновляем last_order_attempt
	_, err := ch.db.Exec(ctx,
		`UPDATE users SET last_order_attempt = NOW() WHERE telegram_id = $1`, userID)
	if err != nil {
		log.Printf("❌ Ошибка обновления last_order_attempt: %v\n", err)
	}

	// Получаем информацию о букете
	row := ch.db.QueryRow(ctx,
		`SELECT name FROM bouquets WHERE id = $1`, draft.BouquetID)
	var bouquetName string
	if err := row.Scan(&bouquetName); err != nil {
		log.Printf("❌ Ошибка получения названия букета: %v\n", err)
		return c.Edit("❌ Ошибка при создании заказа")
	}

	// Создаем заказ в БД
	var orderID, orderNumber int
	var prepayAmount *float64 = nil

	if draft.PaymentType == models.PaymentTypeKaspi && draft.DeliveryType == models.DeliveryTypePickup {
		// 50% предоплата
		prepay := draft.Amount * 0.5
		prepayAmount = &prepay
	} else if draft.PaymentType == models.PaymentTypeKaspi {
		// 100% предоплата
		prepay := draft.Amount
		prepayAmount = &prepay
	}

	var deliveryAddr *string = nil
	if draft.DeliveryType == models.DeliveryTypeDelivery {
		deliveryAddr = &draft.Address
	}

	err = ch.db.QueryRow(ctx,
		`INSERT INTO orders (user_id, bouquet_id, delivery_type, payment_type, amount, prepay_amount,
			customer_name, customer_phone, delivery_address, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
		RETURNING id, order_number`,
		userID, draft.BouquetID, draft.DeliveryType, draft.PaymentType, draft.Amount, prepayAmount,
		draft.Name, draft.Phone, deliveryAddr, models.OrderStatusPending).Scan(&orderID, &orderNumber)

	if err != nil {
		log.Printf("❌ Ошибка создания заказа: %v\n", err)
		return c.Edit("❌ Ошибка при создании заказа")
	}

	log.Printf("✅ Создан заказ #%d пользователем %d\n", orderNumber, userID)

	// Уведомляем админа
	ch.NotifyAdminNewOrder(ctx, orderID, orderNumber, draft, bouquetName)

	// Сохраняем нужные данные до очистки состояния
	paymentType := draft.PaymentType
	amount := draft.Amount
	deliveryType := draft.DeliveryType

	// Очищаем состояние
	ch.stateManager.ResetState(userID)

	if paymentType == models.PaymentTypeCash {
		// Для наличных сразу подтверждение
		return c.Edit(fmt.Sprintf("✅ Заказ #%d принят!\n\n⏳ Ожидайте подтверждения от администратора.", orderNumber))
	} else {
		// Для Kaspi - просим чек
		ch.stateManager.SetState(userID, models.StateAwaitingReceipt)

		// Получаем ссылку на Kaspi
		row := ch.db.QueryRow(ctx, "SELECT kaspi_link FROM shop_settings LIMIT 1")
		var kaspiLink string
		if err := row.Scan(&kaspiLink); err != nil {
			kaspiLink = ""
		}

		// Расписываем таймер на дедлайн чека
		ch.scheduler.ScheduleReceiptDeadline(orderID, userID, 30*time.Minute)

		msg := fmt.Sprintf(
			"💳 <b>Оплатите %g тг через Kaspi:</b>\n\n"+
				"👉 %s\n\n"+
				"📸 <b>После оплаты отправьте в этот чат скриншот чека, PDF-квитанцию или ссылку на чек.</b>\n\n"+
				"⏰ <b>У вас 30 минут</b> для отправки чека.\n\n"+
				"✅ После проверки чека ваш заказ #%d начнет обрабатываться!",
			amount*float64(map[string]float64{models.DeliveryTypeDelivery: 1, models.DeliveryTypePickup: 0.5}[deliveryType]),
			kaspiLink, orderNumber)

		// Добавляем инлайн-кнопку для удобного перехода в приложение Kaspi
		menu := &telebot.ReplyMarkup{}
		if kaspiLink != "" {
			menu.Inline(
				menu.Row(menu.URL("💳 Оплатить в Kaspi", kaspiLink)),
			)
		}

		return c.Edit(msg, &telebot.SendOptions{ParseMode: telebot.ModeHTML, ReplyMarkup: menu})
	}
}

// HandleCancelOrder отменяет заказ
func (ch *ClientHandler) HandleCancelOrder(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID

	draft := ch.stateManager.GetOrderDraft(userID)
	if draft != nil {
		// Снимаем резерв с букета
		_, err := ch.db.Exec(ctx,
			`UPDATE bouquets SET reserved_by = NULL, reserved_until = NULL WHERE id = $1`,
			draft.BouquetID)
		if err != nil {
			log.Printf("❌ Ошибка снятия резерва: %v\n", err)
		}

		// Отменяем таймер
		ch.scheduler.CancelBouquetTimer(draft.BouquetID)
	}

	ch.stateManager.ResetState(userID)
	return c.Edit("❌ Заказ отменён")
}

// HandleReceiptInput обрабатывает получение чека от пользователя
func (ch *ClientHandler) HandleReceiptInput(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID

	// Получаем ID последнего заказа в статусе pending
	row := ch.db.QueryRow(ctx,
		`SELECT id FROM orders WHERE user_id = $1 AND status = $2 AND payment_type = $3
		ORDER BY created_at DESC LIMIT 1`,
		userID, models.OrderStatusPending, models.PaymentTypeKaspi)

	var orderID int
	if err := row.Scan(&orderID); err != nil {
		log.Printf("❌ Ошибка получения заказа: %v\n", err)
		return c.Edit("❌ Активный заказ не найден")
	}

	// Сохраняем URL чека
	receiptURL := ""
	if c.Message().Photo != nil {
		// Если отправлено фото - берем его ID
		receiptURL = c.Message().Photo.FileID
	} else if c.Message().Document != nil {
		// Если отправлен документ - берем его ID
		receiptURL = c.Message().Document.FileID
	} else {
		// Если текст - может быть ссылка
		receiptURL = c.Message().Text
	}

	_, err := ch.db.Exec(ctx,
		`UPDATE orders SET receipt_url = $1, updated_at = NOW() WHERE id = $2`,
		receiptURL, orderID)
	if err != nil {
		log.Printf("❌ Ошибка сохранения чека: %v\n", err)
		return c.Edit("❌ Ошибка при сохранении чека")
	}

	log.Printf("✅ Чек получен для заказа %d\n", orderID)

	// Получаем информацию о заказе
	row = ch.db.QueryRow(ctx,
		`SELECT o.order_number, o.amount, b.name
		FROM orders o
		LEFT JOIN bouquets b ON o.bouquet_id = b.id
		WHERE o.id = $1`, orderID)

	var orderNumber int
	var amount float64
	var bouquetName string
	if err := row.Scan(&orderNumber, &amount, &bouquetName); err != nil {
		log.Printf("❌ Ошибка получения данных заказа: %v\n", err)
	}

	// Отменяем таймер на дедлайн чека
	ch.scheduler.CancelOrderTimer(orderID)

	// Очищаем состояние
	ch.stateManager.ResetState(userID)

	// Уведомляем админа о полученном чеке
	ch.NotifyAdminReceiptReceived(ctx, orderID, orderNumber, userID, receiptURL)

	return c.Edit("✅ Чек отправлен на проверку! Скоро подтвердим. ⏳")
}

// HandleMyOrders показывает заказы пользователя
func (ch *ClientHandler) HandleMyOrders(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID

	rows, err := ch.db.Query(ctx,
		`SELECT o.id, o.order_number, COALESCE(b.name, 'Кастомный букет'), o.status, o.created_at
		FROM orders o
		LEFT JOIN bouquets b ON o.bouquet_id = b.id
		WHERE o.user_id = $1
		ORDER BY o.created_at DESC
		LIMIT 20`, userID)
	if err != nil {
		log.Printf("❌ Ошибка получения заказов: %v\n", err)
		return c.Edit("❌ Ошибка при загрузке заказов")
	}
	defer rows.Close()

	var orders []string
	for rows.Next() {
		var id, orderNumber int
		var name, status string
		var createdAt time.Time

		if err := rows.Scan(&id, &orderNumber, &name, &status, &createdAt); err != nil {
			log.Printf("❌ Ошибка сканирования заказа: %v\n", err)
			continue
		}

		statusEmoji := map[string]string{
			models.OrderStatusPending:    "🟡",
			models.OrderStatusConfirmed:  "🟢",
			models.OrderStatusDelivering: "🚚",
			models.OrderStatusCompleted:  "✅",
			models.OrderStatusCancelled:  "❌",
		}[status]

		orders = append(orders, fmt.Sprintf("%s #%d — %s — %s", statusEmoji, orderNumber, name, createdAt.Format("02.01.2006")))
	}

	menu := &telebot.ReplyMarkup{}
	menu.Inline(menu.Row(telebot.Btn{Text: "⬅️ Главное меню", Unique: "main_menu"}))

	if len(orders) == 0 {
		return c.Edit("📭 У вас еще нет заказов", menu)
	}

	msg := "📦 <b>Ваши заказы:</b>\n\n" + strings.Join(orders, "\n")
	return c.Edit(msg, &telebot.SendOptions{ParseMode: telebot.ModeHTML}, menu)
}

// HandleCustomBouquet обрабатывает запрос на кастомный букет
func (ch *ClientHandler) HandleCustomBouquet(c telebot.Context) error {
	userID := c.Sender().ID
	ch.stateManager.SetState(userID, models.StateAwaitingCustomBouquet)

	menu := &telebot.ReplyMarkup{ForceReply: true}
	// We want to force reply, and also add an inline keyboard? Telebot usually overrides ReplyMarkup if both are used, but often ForceReply works alongside InlineKeyboard if telebot allows.
	// Actually, let's just make it a normal send without ForceReply if it has inline layout, or keep ForceReply.
	menu.Inline(menu.Row(telebot.Btn{Text: "⬅️ Главное меню", Unique: "main_menu"}))
	return c.Send("🎨 Опишите, какой букет вы хотите видеть:\n(цветы, цвета, повод, бюджет, и т.д.)", menu)
}

// HandleCustomBouquetInput обрабатывает описание кастомного букета
func (ch *ClientHandler) HandleCustomBouquetInput(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID
	text := c.Message().Text

	if text == "" || len(text) < 10 {
		return c.Edit("❌ Пожалуйста, напишите подробное описание букета")
	}

	// Сохраняем кастомный букет в БД
	var customOrderID int
	err := ch.db.QueryRow(ctx,
		`INSERT INTO custom_orders (user_id, description, status, created_at)
		VALUES ($1, $2, $3, NOW())
		RETURNING id`,
		userID, text, models.CustomOrderStatusPending).Scan(&customOrderID)

	if err != nil {
		log.Printf("❌ Ошибка создания кастомного букета: %v\n", err)
		return c.Edit("❌ Ошибка при создании заказа")
	}

	log.Printf("✅ Создан кастомный букет #%d пользователем %d\n", customOrderID, userID)

	// Очищаем состояние
	ch.stateManager.ResetState(userID)

	// Уведомляем админа
	ch.NotifyAdminCustomBouquet(ctx, customOrderID, userID, text)

	return c.Edit("✅ Ваша заявка отправлена администратору! 🎨\n\nОжидайте ответа с предложением по цене.")
}

// HandleAbout показывает информацию о магазине
func (ch *ClientHandler) HandleAbout(c telebot.Context) error {
	ctx := context.Background()

	row := ch.db.QueryRow(ctx, "SELECT about_channel_link FROM shop_settings LIMIT 1")
	var link string
	if err := row.Scan(&link); err != nil {
		return c.Edit("ℹ️ Информация о магазине и новые букеты можно найти в нашем канале")
	}

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(telebot.Btn{Text: "📢 Наш канал", URL: link}),
		menu.Row(telebot.Btn{Text: "⬅️ Главное меню", Unique: "main_menu"}),
	)

	return c.Edit("ℹ️ Подпишитесь на наш канал для новых букетов и информации:", menu)
}

// HandleAddress показывает адрес магазина
func (ch *ClientHandler) HandleAddress(c telebot.Context) error {
	ctx := context.Background()

	row := ch.db.QueryRow(ctx, "SELECT address FROM shop_settings LIMIT 1")
	var address string
	if err := row.Scan(&address); err != nil {
		return c.Edit("📍 Адрес магазина не установлен")
	}

	menu := &telebot.ReplyMarkup{}
	menu.Inline(menu.Row(telebot.Btn{Text: "⬅️ Главное меню", Unique: "main_menu"}))
	return c.Edit(fmt.Sprintf("📍 <b>Наш адрес:</b>\n%s", address), &telebot.SendOptions{ParseMode: telebot.ModeHTML}, menu)
}

// HandleSupport показывает кнопку поддержки
func (ch *ClientHandler) HandleSupport(c telebot.Context) error {
	ctx := context.Background()

	row := ch.db.QueryRow(ctx, "SELECT support_user_id FROM shop_settings LIMIT 1")
	var supportID *int64
	if err := row.Scan(&supportID); err != nil || supportID == nil {
		return c.Edit("💬 Служба поддержки временно недоступна")
	}

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(telebot.Btn{Text: "💬 Написать в поддержку", URL: fmt.Sprintf("tg://user?id=%d", *supportID)}),
		menu.Row(telebot.Btn{Text: "⬅️ Главное меню", Unique: "main_menu"}),
	)

	return c.Edit("Нажмите на кнопку, чтобы написать нашей службе поддержки:", menu)
}

// NotifyAdminNewOrder уведомляет админа о новом заказе
func (ch *ClientHandler) NotifyAdminNewOrder(ctx context.Context, orderID int, orderNumber int, draft *models.OrderDraft, bouquetName string) {
	msg := fmt.Sprintf(
		"🔔 <b>Новый заказ #%d</b>\n\n"+
			"👤 <b>%s</b> | %s\n"+
			"🌸 <b>%s</b>\n"+
			"📦 %s\n"+
			"💳 %s\n"+
			"💰 <b>Сумма: %g тг</b>",
		orderNumber, draft.Name, draft.Phone, bouquetName,
		map[string]string{models.DeliveryTypeDelivery: "🚚 Доставка", models.DeliveryTypePickup: "🏪 Самовывоз"}[draft.DeliveryType],
		map[string]string{models.PaymentTypeKaspi: "💳 Kaspi Pay", models.PaymentTypeCash: "💵 Наличные"}[draft.PaymentType],
		draft.Amount)

	if draft.DeliveryType == models.DeliveryTypeDelivery {
		msg += fmt.Sprintf("\n📍 <b>Адрес:</b> %s", draft.Address)
	}

	// Отправляем всем админам
	for _, adminID := range ch.adminIDs {
		admin := &telebot.User{ID: adminID}
		if _, err := ch.bot.Send(admin, msg, &telebot.SendOptions{ParseMode: telebot.ModeHTML}); err != nil {
			log.Printf("❌ Ошибка отправки уведомления админу %d: %v\n", adminID, err)
		}
	}
}

// NotifyAdminReceiptReceived уведомляет админа о полученном чеке
func (ch *ClientHandler) NotifyAdminReceiptReceived(ctx context.Context, orderID int, orderNumber int, userID int64, receiptURL string) {
	// Получаем информацию о пользователе
	row := ch.db.QueryRow(ctx, "SELECT full_name, phone FROM users WHERE telegram_id = $1", userID)
	var userName, phone string
	if err := row.Scan(&userName, &phone); err != nil {
		userName = "Unknown"
		phone = ""
	}

	msg := fmt.Sprintf(
		"💰 <b>Пришёл чек по заказу #%d</b>\n\n"+
			"👤 <b>%s</b> | %s\n\n",
		orderNumber, userName, phone)

	// Отправляем всем админам
	for _, adminID := range ch.adminIDs {
		admin := &telebot.User{ID: adminID}

		// Отправляем текст
		_, err := ch.bot.Send(admin, msg, &telebot.SendOptions{ParseMode: telebot.ModeHTML})
		if err != nil {
			log.Printf("❌ Ошибка отправки уведомления админу %d: %v\n", adminID, err)
			continue
		}

		// Отправляем чек если это фото/документ
		if receiptURL != "" && len(receiptURL) > 10 {
			// Пытаемся отправить как фото
			photo := &telebot.Photo{File: telebot.File{FileID: receiptURL}}
			if _, err := ch.bot.Send(admin, photo); err != nil {
				// Если не получилось - просто сообщение с URL
				_, _ = ch.bot.Send(admin, fmt.Sprintf("📎 Чек: %s", receiptURL))
			}
		}

		// Отправляем кнопки действия
		menu := &telebot.ReplyMarkup{}
		menu.Inline(
			menu.Row(
				telebot.Btn{Text: "✅ Оплата получена", Unique: fmt.Sprintf("admin_payment_accept_%d", orderID)},
				telebot.Btn{Text: "❌ Оплата не получена", Unique: fmt.Sprintf("admin_payment_reject_%d", orderID)},
			),
		)

		_, err = ch.bot.Send(admin, "Что дальше?", menu)
		if err != nil {
			log.Printf("❌ Ошибка отправки кнопок админу %d: %v\n", adminID, err)
		}
	}
}

// NotifyAdminCustomBouquet уведомляет админа о новом кастомном букете
func (ch *ClientHandler) NotifyAdminCustomBouquet(ctx context.Context, customOrderID int, userID int64, description string) {
	// Получаем информацию о пользователе
	row := ch.db.QueryRow(ctx, "SELECT full_name, phone FROM users WHERE telegram_id = $1", userID)
	var userName, phone string
	if err := row.Scan(&userName, &phone); err != nil {
		userName = "Unknown"
		phone = ""
	}

	msg := fmt.Sprintf(
		"🎨 <b>Новый кастомный букет #%d</b>\n\n"+
			"👤 <b>%s</b> | %s\n\n"+
			"📝 <b>Описание:</b>\n%s",
		customOrderID, userName, phone, description)

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			telebot.Btn{Text: "✅ Принять и назначить цену", Unique: fmt.Sprintf("admin_custom_accept_%d", customOrderID)},
			telebot.Btn{Text: "❌ Отклонить", Unique: fmt.Sprintf("admin_custom_reject_%d", customOrderID)},
		),
	)

	// Отправляем всем админам
	for _, adminID := range ch.adminIDs {
		admin := &telebot.User{ID: adminID}
		if _, err := ch.bot.Send(admin, msg, &telebot.SendOptions{ParseMode: telebot.ModeHTML}, menu); err != nil {
			log.Printf("❌ Ошибка отправки уведомления админу %d: %v\n", adminID, err)
		}
	}
}
