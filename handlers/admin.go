package handlers

import (
	"context"
	"fmt"
	"io"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"flower-bot/db"
	"flower-bot/models"
	"flower-bot/scheduler"
	"flower-bot/services"

	"gopkg.in/telebot.v3"
)

// AdminHandler обрабатывает запросы администраторов
type AdminHandler struct {
	db           *db.Database
	bot          *telebot.Bot
	stateManager *StateManager
	scheduler    *scheduler.Scheduler
	adminIDs     []int64
}

// NewAdminHandler создает новый обработчик админа
func NewAdminHandler(database *db.Database, bot *telebot.Bot, sm *StateManager, sch *scheduler.Scheduler, adminIDs []int64) *AdminHandler {
	return &AdminHandler{
		db:           database,
		bot:          bot,
		stateManager: sm,
		scheduler:    sch,
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
	shopSettingsMap, err := ah.db.GetShopSettings(ctx)
	shopName := "Flower Shop"
	address := "Не указан"
	if err == nil {
		if name, ok := shopSettingsMap["shop_name"].(string); ok {
			shopName = name
		}
		if addr, ok := shopSettingsMap["address"].(string); ok {
			address = addr
		}
	}

	// Активные заказы
	pendingRow := ah.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM orders WHERE status IN ('pending', 'confirmed', 'delivering')`)
	var pendingCount int
	pendingRow.Scan(&pendingCount)

	// Букеты
	bouquetRow := ah.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM bouquets WHERE is_available = true`)
	var bouquetCount int
	bouquetRow.Scan(&bouquetCount)

	// Пользователи
	usersRow := ah.db.QueryRow(ctx, `SELECT COUNT(*) FROM users`)
	var usersCount int
	usersRow.Scan(&usersCount)

	// Текст панели
	text := fmt.Sprintf(
		"<b>🌸 АДМИН ПАНЕЛЬ 🌸</b>\n\n"+
			"<b>Магазин:</b> %s\n"+
			"<b>Адрес:</b> %s\n\n"+
			"<b>📊 СТАТИСТИКА:</b>\n"+
			"🔴 Активные заказы: <b>%d</b>\n"+
			"🌹 Букетов в каталоге: <b>%d</b>\n"+
			"👥 Зарегистрировано пользователей: <b>%d</b>\n\n"+
			"<b>⚡ БЫСТРЫЕ ДЕЙСТВИЯ:</b>\n",
		shopName, address, pendingCount, bouquetCount, usersCount)

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			telebot.Btn{Text: "📦 Заказы", Unique: "admin_orders"},
			telebot.Btn{Text: "🌸 Каталог", Unique: "admin_catalog"},
		),
		menu.Row(
			telebot.Btn{Text: "⚙️ Настройки", Unique: "admin_settings"},
			telebot.Btn{Text: "📊 Статистика", Unique: "admin_stats"},
		),
	)

	return c.Send(text, menu)
}

// ========== ЗАКАЗЫ ==========

// HandleAdminOrders показывает список заказов
func (ah *AdminHandler) HandleAdminOrders(c telebot.Context) error {
	ctx := context.Background()

	rows, err := ah.db.Query(ctx,
		`SELECT id, order_number, customer_name, status FROM orders
		WHERE status != $1 AND status != $2
		ORDER BY created_at DESC
		LIMIT 20`,
		models.OrderStatusCompleted, models.OrderStatusCancelled)
	if err != nil {
		log.Printf("❌ Ошибка получения заказов: %v\n", err)
		return c.Edit("❌ Ошибка при загрузке заказов")
	}
	defer rows.Close()

	menu := &telebot.ReplyMarkup{}

	for rows.Next() {
		var id, orderNumber int
		var customerName, status string

		if err := rows.Scan(&id, &orderNumber, &customerName, &status); err != nil {
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

		menu.Inline(
			menu.Row(
				telebot.Btn{
					Text:   fmt.Sprintf("%s #%d — %s", statusEmoji, orderNumber, customerName),
					Unique: fmt.Sprintf("admin_order_detail_%d", id),
				},
			),
		)
	}

	return c.Edit("📦 <b>Активные заказы:</b>", menu)
}

// HandleAdminOrderDetail показывает детали заказа
func (ah *AdminHandler) HandleAdminOrderDetail(c telebot.Context, orderID int) error {
	ctx := context.Background()

	row := ah.db.QueryRow(ctx,
		`SELECT o.id, o.order_number, o.customer_name, o.customer_phone, o.delivery_type,
			o.payment_type, o.amount, o.delivery_address, o.status,
			COALESCE(b.name, 'Кастомный букет') as bouquet_name
		FROM orders o
		LEFT JOIN bouquets b ON o.bouquet_id = b.id
		WHERE o.id = $1`, orderID)

	var id, orderNumber int
	var customerName, customerPhone, deliveryType, paymentType, deliveryAddress, status, bouquetName string
	var amount float64

	if err := row.Scan(&id, &orderNumber, &customerName, &customerPhone, &deliveryType,
		&paymentType, &amount, &deliveryAddress, &status, &bouquetName); err != nil {
		log.Printf("❌ Ошибка получения заказа: %v\n", err)
		return c.Edit("❌ Заказ не найден")
	}

	msg := fmt.Sprintf(
		"📦 <b>Заказ #%d</b>\n\n"+
			"👤 <b>%s</b> | %s\n"+
			"🌸 <b>%s</b>\n"+
			"📦 %s\n"+
			"💳 %s\n"+
			"💰 <b>Сумма:</b> %g тг\n"+
			"📊 <b>Статус:</b> %s",
		orderNumber, customerName, customerPhone, bouquetName,
		map[string]string{models.DeliveryTypeDelivery: "🚚 Доставка", models.DeliveryTypePickup: "🏪 Самовывоз"}[deliveryType],
		map[string]string{models.PaymentTypeKaspi: "💳 Kaspi Pay", models.PaymentTypeCash: "💵 Наличные"}[paymentType],
		amount, status)

	if deliveryType == models.DeliveryTypeDelivery {
		msg += fmt.Sprintf("\n📍 <b>Адрес:</b> %s", deliveryAddress)
	}

	menu := &telebot.ReplyMarkup{}

	switch status {
	case models.OrderStatusPending:
		menu.Inline(
			menu.Row(
				telebot.Btn{Text: "✅ Подтвердить", Unique: fmt.Sprintf("admin_confirm_order_%d", id)},
				telebot.Btn{Text: "❌ Отменить", Unique: fmt.Sprintf("admin_cancel_order_%d", id)},
			),
		)
	case models.OrderStatusConfirmed:
		menu.Inline(
			menu.Row(
				telebot.Btn{Text: "🚚 Отправлен курьером", Unique: fmt.Sprintf("admin_delivering_%d", id)},
				telebot.Btn{Text: "✅ Отдан клиенту", Unique: fmt.Sprintf("admin_complete_%d", id)},
			),
		)
	case models.OrderStatusDelivering:
		menu.Inline(
			menu.Row(
				telebot.Btn{Text: "✅ Отдан клиенту", Unique: fmt.Sprintf("admin_complete_%d", id)},
			),
		)
	}

	return c.Edit(msg, &telebot.SendOptions{ParseMode: telebot.ModeHTML}, menu)
}

// HandleAdminConfirmOrder подтверждает заказ
func (ah *AdminHandler) HandleAdminConfirmOrder(c telebot.Context, orderID int) error {
	// Двойное подтверждение
	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			telebot.Btn{Text: "✅ Да, подтвердить", Unique: fmt.Sprintf("admin_confirm_yes_%d", orderID)},
			telebot.Btn{Text: "❌ Отмена", Unique: fmt.Sprintf("admin_cancel_confirm_%d", orderID)},
		),
	)

	return c.Edit("❓ Подтвердить заказ?", menu)
}

// HandleAdminConfirmYes finalizes confirm
func (ah *AdminHandler) HandleAdminConfirmOrderYes(c telebot.Context, orderID int) error {
	ctx := context.Background()

	// Получаем информацию о заказе
	row := ah.db.QueryRow(ctx,
		`SELECT order_number, user_id, bouquet_id, payment_type
		FROM orders WHERE id = $1`, orderID)

	var orderNumber int
	var userID int64
	var bouquetID *int
	var paymentType string

	if err := row.Scan(&orderNumber, &userID, &bouquetID, &paymentType); err != nil {
		log.Printf("❌ Ошибка получения заказа: %v\n", err)
		return c.Edit("❌ Ошибка при подтверждении заказа")
	}

	// Обновляем статус заказа
	_, err := ah.db.Exec(ctx,
		`UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2`,
		models.OrderStatusConfirmed, orderID)
	if err != nil {
		log.Printf("❌ Ошибка обновления заказа: %v\n", err)
		return c.Edit("❌ Ошибка при подтверждении заказа")
	}

	// Если был букет из каталога - уменьшаем количество
	if bouquetID != nil && *bouquetID > 0 {
		_, err := ah.db.Exec(ctx,
			`UPDATE bouquets SET quantity = quantity - 1, is_available = (quantity - 1) > 0, 
			reserved_by = NULL, reserved_until = NULL WHERE id = $1`, *bouquetID)
		if err != nil {
			log.Printf("❌ Ошибка обновления количества букета: %v\n", err)
		}

		// Отменяем таймер на резерв
		ah.scheduler.CancelBouquetTimer(*bouquetID)
	}

	// Уведомляем пользователя
	user := &telebot.User{ID: userID}
	msg := fmt.Sprintf("✅ Ваш заказ #%d подтверждён! Начинаем подготовку. 🎊", orderNumber)
	if _, err := ah.bot.Send(user, msg); err != nil {
		log.Printf("❌ Ошибка отправки уведомления пользователю: %v\n", err)
	}

	log.Printf("✅ Заказ #%d подтверждён\n", orderNumber)

	return c.Edit(fmt.Sprintf("✅ Заказ #%d подтверждён", orderNumber))
}

// HandleAdminCancelOrder отменяет заказ
func (ah *AdminHandler) HandleAdminCancelOrder(c telebot.Context, orderID int) error {
	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			telebot.Btn{Text: "✅ Да, отменить", Unique: fmt.Sprintf("admin_cancel_yes_%d", orderID)},
			telebot.Btn{Text: "❌ Отмена", Unique: fmt.Sprintf("admin_cancel_confirm_%d", orderID)},
		),
	)

	return c.Edit("❓ Отменить заказ?", menu)
}

// HandleAdminCancelOrderYes finalizes cancel
func (ah *AdminHandler) HandleAdminCancelOrderYes(c telebot.Context, orderID int) error {
	ctx := context.Background()

	// Получаем информацию о заказе
	row := ah.db.QueryRow(ctx,
		`SELECT order_number, user_id, bouquet_id
		FROM orders WHERE id = $1`, orderID)

	var orderNumber int
	var userID int64
	var bouquetID *int

	if err := row.Scan(&orderNumber, &userID, &bouquetID); err != nil {
		log.Printf("❌ Ошибка получения заказа: %v\n", err)
		return c.Edit("❌ Ошибка при отмене заказа")
	}

	// Обновляем статус заказа
	_, err := ah.db.Exec(ctx,
		`UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2`,
		models.OrderStatusCancelled, orderID)
	if err != nil {
		log.Printf("❌ Ошибка обновления заказа: %v\n", err)
		return c.Edit("❌ Ошибка при отмене заказа")
	}

	// Если был букет из каталога - восстанавливаем перезарезервирование
	if bouquetID != nil && *bouquetID > 0 {
		_, err := ah.db.Exec(ctx,
			`UPDATE bouquets SET is_available = true, reserved_by = NULL, reserved_until = NULL
			WHERE id = $1`, *bouquetID)
		if err != nil {
			log.Printf("❌ Ошибка обновления букета: %v\n", err)
		}

		// Отменяем таймер на резерв
		ah.scheduler.CancelBouquetTimer(*bouquetID)
	}

	// Уведомляем пользователя
	user := &telebot.User{ID: userID}
	msg := fmt.Sprintf("❌ Заказ #%d отменён.\n\nВы можете создать новый заказ.", orderNumber)
	if _, err := ah.bot.Send(user, msg); err != nil {
		log.Printf("❌ Ошибка отправки уведомления пользователю: %v\n", err)
	}

	log.Printf("✅ Заказ #%d отменён\n", orderNumber)

	return c.Edit(fmt.Sprintf("✅ Заказ #%d отменён", orderNumber))
}

// HandleAdminDelivering обновляет статус на доставку
func (ah *AdminHandler) HandleAdminDelivering(c telebot.Context, orderID int) error {
	ctx := context.Background()

	row := ah.db.QueryRow(ctx, "SELECT order_number, user_id FROM orders WHERE id = $1", orderID)
	var orderNumber int
	var userID int64
	if err := row.Scan(&orderNumber, &userID); err != nil {
		return c.Edit("❌ Заказ не найден")
	}

	_, err := ah.db.Exec(ctx,
		`UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2`,
		models.OrderStatusDelivering, orderID)
	if err != nil {
		return c.Edit("❌ Ошибка обновления")
	}

	// Уведомляем пользователя
	user := &telebot.User{ID: userID}
	msg := fmt.Sprintf("🚚 Заказ #%d отправлен в доставку! Курьер скоро будет у вас.", orderNumber)
	if _, err := ah.bot.Send(user, msg); err != nil {
		log.Printf("❌ Ошибка отправки уведомления: %v\n", err)
	}

	return c.Edit(fmt.Sprintf("✅ Заказ #%d в доставке", orderNumber))
}

// HandleAdminComplete завершает заказ
func (ah *AdminHandler) HandleAdminComplete(c telebot.Context, orderID int) error {
	ctx := context.Background()

	row := ah.db.QueryRow(ctx, "SELECT order_number, user_id FROM orders WHERE id = $1", orderID)
	var orderNumber int
	var userID int64
	if err := row.Scan(&orderNumber, &userID); err != nil {
		return c.Edit("❌ Заказ не найден")
	}

	_, err := ah.db.Exec(ctx,
		`UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2`,
		models.OrderStatusCompleted, orderID)
	if err != nil {
		return c.Edit("❌ Ошибка обновления")
	}

	// Уведомляем пользователя
	user := &telebot.User{ID: userID}
	msg := fmt.Sprintf("✅ Заказ #%d выполнен! Спасибо за покупку! 🌸", orderNumber)
	if _, err := ah.bot.Send(user, msg); err != nil {
		log.Printf("❌ Ошибка отправки уведомления: %v\n", err)
	}

	return c.Edit(fmt.Sprintf("✅ Заказ #%d завершён", orderNumber))
}

// HandleAdminPaymentAccept принимает оплату
func (ah *AdminHandler) HandleAdminPaymentAccept(c telebot.Context, orderID int) error {
	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			telebot.Btn{Text: "✅ Да, оплата получена", Unique: fmt.Sprintf("admin_accept_yes_%d", orderID)},
			telebot.Btn{Text: "❌ Отмена", Unique: fmt.Sprintf("admin_cancel_confirm_%d", orderID)},
		),
	)

	return c.Edit("❓ Подтвердить получение оплаты?", menu)
}

// HandleAdminPaymentAcceptYes finalizes payment accept
func (ah *AdminHandler) HandleAdminPaymentAcceptYes(c telebot.Context, orderID int) error {
	ctx := context.Background()

	row := ah.db.QueryRow(ctx, "SELECT order_number, user_id, bouquet_id FROM orders WHERE id = $1", orderID)
	var orderNumber int
	var userID int64
	var bouquetID *int

	if err := row.Scan(&orderNumber, &userID, &bouquetID); err != nil {
		return c.Edit("❌ Заказ не найден")
	}

	// Обновляем статус
	_, err := ah.db.Exec(ctx,
		`UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2`,
		models.OrderStatusConfirmed, orderID)
	if err != nil {
		return c.Edit("❌ Ошибка обновления")
	}

	// Уменьшаем количество букета
	if bouquetID != nil && *bouquetID > 0 {
		_, err := ah.db.Exec(ctx,
			`UPDATE bouquets SET quantity = quantity - 1, is_available = (quantity - 1) > 0,
			reserved_by = NULL, reserved_until = NULL WHERE id = $1`, *bouquetID)
		if err != nil {
			log.Printf("❌ Ошибка обновления букета: %v\n", err)
		}
	}

	// Отменяем таймер на чек
	ah.scheduler.CancelOrderTimer(orderID)

	// Уведомляем пользователя
	user := &telebot.User{ID: userID}
	msg := fmt.Sprintf("✅ Оплата подтверждена! Заказ #%d в работе. 🎊", orderNumber)
	if _, err := ah.bot.Send(user, msg); err != nil {
		log.Printf("❌ Ошибка отправки уведомления: %v\n", err)
	}

	return c.Edit(fmt.Sprintf("✅ Оплата по заказу #%d принята", orderNumber))
}

// HandleAdminPaymentReject отклоняет оплату
func (ah *AdminHandler) HandleAdminPaymentReject(c telebot.Context, orderID int) error {
	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			telebot.Btn{Text: "✅ Да, отклонить", Unique: fmt.Sprintf("admin_reject_yes_%d", orderID)},
			telebot.Btn{Text: "❌ Отмена", Unique: fmt.Sprintf("admin_cancel_confirm_%d", orderID)},
		),
	)

	return c.Edit("❓ Отклонить оплату?", menu)
}

// HandleAdminPaymentRejectYes finalizes payment reject
func (ah *AdminHandler) HandleAdminPaymentRejectYes(c telebot.Context, orderID int) error {
	ctx := context.Background()

	row := ah.db.QueryRow(ctx, "SELECT order_number, user_id, bouquet_id FROM orders WHERE id = $1", orderID)
	var orderNumber int
	var userID int64
	var bouquetID *int

	if err := row.Scan(&orderNumber, &userID, &bouquetID); err != nil {
		return c.Edit("❌ Заказ не найден")
	}

	// Отменяем заказ
	_, err := ah.db.Exec(ctx,
		`UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2`,
		models.OrderStatusCancelled, orderID)
	if err != nil {
		return c.Edit("❌ Ошибка обновления")
	}

	// Возвращаем букет в каталог
	if bouquetID != nil && *bouquetID > 0 {
		_, err := ah.db.Exec(ctx,
			`UPDATE bouquets SET is_available = true, reserved_by = NULL, reserved_until = NULL
			WHERE id = $1`, *bouquetID)
		if err != nil {
			log.Printf("❌ Ошибка обновления букета: %v\n", err)
		}

		ah.scheduler.CancelBouquetTimer(*bouquetID)
	}

	// Отменяем таймер на чек
	ah.scheduler.CancelOrderTimer(orderID)

	// Уведомляем пользователя
	user := &telebot.User{ID: userID}
	msg := fmt.Sprintf("❌ Оплата не была подтверждена. Заказ #%d отменён.\n\nБукет возвращён в каталог.", orderNumber)
	if _, err := ah.bot.Send(user, msg); err != nil {
		log.Printf("❌ Ошибка отправки уведомления: %v\n", err)
	}

	return c.Edit(fmt.Sprintf("✅ Оплата по заказу #%d отклонена", orderNumber))
}

// ========== КАТАЛОГ ==========

// HandleAdminCatalog показывает каталог для админа
func (ah *AdminHandler) HandleAdminCatalog(c telebot.Context) error {
	ctx := context.Background()

	rows, err := ah.db.Query(ctx,
		`SELECT id, name, quantity, is_available FROM bouquets
		ORDER BY created_at DESC
		LIMIT 20`)
	if err != nil {
		log.Printf("❌ Ошибка получения букетов: %v\n", err)
		return c.Edit("❌ Ошибка при загрузке каталога")
	}
	defer rows.Close()

	menu := &telebot.ReplyMarkup{}

	for rows.Next() {
		var id, quantity int
		var name string
		var isAvailable bool

		if err := rows.Scan(&id, &name, &quantity, &isAvailable); err != nil {
			log.Printf("❌ Ошибка сканирования букета: %v\n", err)
			continue
		}

		statusEmoji := "✅"
		if !isAvailable {
			statusEmoji = "❌"
		}

		menu.Inline(
			menu.Row(
				telebot.Btn{
					Text:   fmt.Sprintf("%s %s (%d)", statusEmoji, name, quantity),
					Unique: fmt.Sprintf("admin_bouquet_detail_%d", id),
				},
			),
		)
	}

	menu.Inline(
		menu.Row(
			telebot.Btn{Text: "➕ Добавить букет", Unique: "admin_add_bouquet"},
		),
	)

	return c.Edit("🌸 <b>Каталог букетов:</b>", menu)
}

// HandleAdminBouquetDetail показывает детали букета
func (ah *AdminHandler) HandleAdminBouquetDetail(c telebot.Context, bouquetID int) error {
	ctx := context.Background()

	row := ah.db.QueryRow(ctx,
		`SELECT id, name, description, price, quantity, is_available FROM bouquets WHERE id = $1`,
		bouquetID)

	var id, quantity int
	var name, description string
	var price float64
	var isAvailable bool

	if err := row.Scan(&id, &name, &description, &price, &quantity, &isAvailable); err != nil {
		log.Printf("❌ Ошибка получения букета: %v\n", err)
		return c.Edit("❌ Букет не найден")
	}

	msg := fmt.Sprintf(
		"🌸 <b>%s</b>\n\n"+
			"📝 %s\n"+
			"💰 <b>Цена:</b> %g тг\n"+
			"📦 <b>Кол-во:</b> %d шт\n"+
			"📊 <b>Статус:</b> %s",
		name, description, price, quantity,
		map[bool]string{true: "✅ Доступен", false: "❌ Скрыт"}[isAvailable])

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			telebot.Btn{Text: "✏️ Редактировать", Unique: fmt.Sprintf("admin_edit_bouquet_%d", id)},
			telebot.Btn{Text: "🗑 Удалить", Unique: fmt.Sprintf("admin_delete_bouquet_%d", id)},
		),
		menu.Row(
			telebot.Btn{
				Text:   map[bool]string{true: "👁 Скрыть", false: "👁 Показать"}[isAvailable],
				Unique: fmt.Sprintf("admin_toggle_bouquet_%d", id),
			},
		),
	)

	return c.Edit(msg, &telebot.SendOptions{ParseMode: telebot.ModeHTML}, menu)
}

// HandleAdminAddBouquet начинает добавление букета
func (ah *AdminHandler) HandleAdminAddBouquet(c telebot.Context) error {
	userID := c.Sender().ID
	ah.stateManager.SetState(userID, models.StateAdminAddBouquetName)

	menu := &telebot.ReplyMarkup{ForceReply: true}
	return c.Send("🌸 Введите название букета:", menu)
}

// HandleAdminBouquetNameInput обрабатывает название букета
func (ah *AdminHandler) HandleAdminBouquetNameInput(c telebot.Context) error {
	userID := c.Sender().ID
	text := c.Message().Text

	if text == "" || len(text) < 2 {
		return c.Send("❌ Пожалуйста, введите корректное название")
	}

	ah.stateManager.SetTempData(userID, text)
	ah.stateManager.SetState(userID, models.StateAdminAddBouquetDesc)

	menu := &telebot.ReplyMarkup{ForceReply: true}
	return c.Send("📝 Введите описание букета:", menu)
}

// HandleAdminBouquetDescInput обрабатывает описание букета
func (ah *AdminHandler) HandleAdminBouquetDescInput(c telebot.Context) error {
	userID := c.Sender().ID
	text := c.Message().Text

	if text == "" || len(text) < 5 {
		return c.Send("❌ Пожалуйста, введите подробное описание")
	}

	// Сохраняем название, которое было до этого
	tempData := ah.stateManager.GetTempData(userID)
	ah.stateManager.SetTempData(userID, fmt.Sprintf("%s|||%s", tempData, text))
	ah.stateManager.SetState(userID, models.StateAdminAddBouquetPrice)

	menu := &telebot.ReplyMarkup{ForceReply: true}
	return c.Send("💰 Введите цену (в тенге):", menu)
}

// HandleAdminBouquetPriceInput обрабатывает цену букета
func (ah *AdminHandler) HandleAdminBouquetPriceInput(c telebot.Context) error {
	userID := c.Sender().ID
	text := c.Message().Text

	// Валидируем цену
	priceRegex := regexp.MustCompile(`^[\d.]+$`)
	if !priceRegex.MatchString(text) {
		return c.Send("❌ Пожалуйста, введите корректную цену (только числа)")
	}

	tempData := ah.stateManager.GetTempData(userID)
	ah.stateManager.SetTempData(userID, fmt.Sprintf("%s|||%s", tempData, text))
	ah.stateManager.SetState(userID, models.StateAdminAddBouquetQty)

	menu := &telebot.ReplyMarkup{ForceReply: true}
	return c.Send("📦 Введите количество bukетов в наличии:", menu)
}

// HandleAdminBouquetQtyInput обрабатывает количество букетов
func (ah *AdminHandler) HandleAdminBouquetQtyInput(c telebot.Context) error {
	userID := c.Sender().ID
	text := c.Message().Text

	// Валидируем количество
	qtyRegex := regexp.MustCompile(`^\d+$`)
	if !qtyRegex.MatchString(text) {
		return c.Send("❌ Пожалуйста, введите корректное количество (только числа)")
	}

	tempData := ah.stateManager.GetTempData(userID)
	ah.stateManager.SetTempData(userID, fmt.Sprintf("%s|||%s", tempData, text))
	ah.stateManager.SetState(userID, models.StateAdminAddBouquetPhoto)

	menu := &telebot.ReplyMarkup{RemoveKeyboard: true}
	btnDone := menu.Text("✅ Готово")
	menu.Reply(
		menu.Row(btnDone),
	)

	return c.Send("📸 Отправьте фото букета (до 5 штук, можно альбомом). После отправки всех фото нажмите кнопку \"✅ Готово\":", menu)
}

// HandleAdminBouquetPhotoInput обрабатывает фото букета
func (ah *AdminHandler) HandleAdminBouquetPhotoInput(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID

	tempData := ah.stateManager.GetTempData(userID)
	parts := strings.Split(tempData, "|||")

	// Если пользователь нажал кнопку Готово
	if c.Message().Text == "✅ Готово" {
		if len(parts) < 5 {
			return c.Send("❌ Вы не добавили ни одного фото. Отправьте хотя бы одно фото.", &telebot.ReplyMarkup{RemoveKeyboard: true})
		}

		name := parts[0]
		description := parts[1]
		price, _ := strconv.ParseFloat(parts[2], 64)
		qty, _ := strconv.ParseInt(parts[3], 10, 64)

		var photoURLs []string
		for i := 4; i < len(parts); i++ {
			if parts[i] != "" {
				photoURLs = append(photoURLs, parts[i])
			}
		}

		// Добавляем букет в БД
		var bouquetID int
		err := ah.db.QueryRow(ctx,
			`INSERT INTO bouquets (name, description, price, photo_urls, quantity, is_available, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, NOW())
			RETURNING id`,
			name, description, price, photoURLs, qty, true).Scan(&bouquetID)

		if err != nil {
			log.Printf("❌ Ошибка добавления букета: %v\n", err)
			return c.Send("❌ Ошибка при добавлении букета", &telebot.ReplyMarkup{RemoveKeyboard: true})
		}

		log.Printf("✅ Добавлен букет #%d\n", bouquetID)
		ah.stateManager.ResetState(userID)

		msg := fmt.Sprintf("✅ <b>Букет добавлен!</b>\n\n"+
			"🌸 %s\n"+
			"💰 %g тг\n"+
			"📦 %d шт",
			name, price, qty)

		return c.Send(msg, &telebot.SendOptions{ParseMode: telebot.ModeHTML}, &telebot.ReplyMarkup{RemoveKeyboard: true})
	}

	// Обработка загрузки фото
	if len(parts) >= 9 {
		return c.Send("❌ Вы уже загрузили 5 фото. Нажмите кнопку '✅ Готово'.")
	}

	var extPhotoURL string
	if c.Message().Photo != nil {
		file, err := ah.bot.FileByID(c.Message().Photo.FileID)
		if err != nil {
			return c.Send("❌ Ошибка получения файла от Telegram")
		}

		rc, err := ah.bot.File(&file)
		if err != nil {
			return c.Send("❌ Ошибка скачивания файла")
		}
		defer rc.Close()

		fileBytes, err := io.ReadAll(rc)
		if err != nil {
			return c.Send("❌ Ошибка чтения файла")
		}

		fileName := fmt.Sprintf("product_%d_%s.jpg", time.Now().UnixNano(), c.Message().Photo.FileID[:10])
		s3url, err := services.UploadFileToS3(fileBytes, fileName, "image/jpeg", "products")
		if err != nil {
			log.Printf("S3 upload err: %v", err)
			return c.Send("❌ Ошибка загрузки в S3")
		}
		extPhotoURL = s3url
	} else if c.Message().Text != "" {
		extPhotoURL = c.Message().Text
	} else {
		return c.Send("❌ Пожалуйста, отправьте фото как картинку.")
	}

	ah.stateManager.SetTempData(userID, fmt.Sprintf("%s|||%s", tempData, extPhotoURL))

	newParts := strings.Split(ah.stateManager.GetTempData(userID), "|||")
	photosCount := len(newParts) - 4

	return c.Send(fmt.Sprintf("✅ Фото добавлено (%d/5). Отправьте еще или нажмите '✅ Готово'.", photosCount))
}

// HandleAdminToggleBouquet скрывает/показывает букет
func (ah *AdminHandler) HandleAdminToggleBouquet(c telebot.Context, bouquetID int) error {
	ctx := context.Background()

	row := ah.db.QueryRow(ctx, "SELECT is_available FROM bouquets WHERE id = $1", bouquetID)
	var isAvailable bool
	if err := row.Scan(&isAvailable); err != nil {
		return c.Edit("❌ Букет не найден")
	}

	_, err := ah.db.Exec(ctx,
		`UPDATE bouquets SET is_available = $1 WHERE id = $2`,
		!isAvailable, bouquetID)
	if err != nil {
		return c.Edit("❌ Ошибка обновления")
	}

	msg := fmt.Sprintf("✅ Букет %s", map[bool]string{true: "показан", false: "скрыт"}[!isAvailable])
	return c.Edit(msg)
}

// HandleAdminDeleteBouquet удаляет букет
func (ah *AdminHandler) HandleAdminDeleteBouquet(c telebot.Context, bouquetID int) error {
	ctx := context.Background()

	_, err := ah.db.Exec(ctx, "DELETE FROM bouquets WHERE id = $1", bouquetID)
	if err != nil {
		return c.Edit("❌ Ошибка удаления")
	}

	return c.Edit("✅ Букет удален")
}

// ========== КАСТОМНЫЕ ЗАКАЗЫ ==========

// HandleAdminCustomAccept принимает кастомный букет
func (ah *AdminHandler) HandleAdminCustomAccept(c telebot.Context, customOrderID int) error {
	userID := c.Sender().ID
	ah.stateManager.SetState(userID, models.StateAdminSetPrice)
	ah.stateManager.SetTempData(userID, fmt.Sprintf("custom_%d", customOrderID))

	menu := &telebot.ReplyMarkup{ForceReply: true}
	return c.Send("💰 Введите цену за этот букет (в тенге):", menu)
}

// HandleAdminCustomPriceInput обрабатывает цену для кастомного букета
func (ah *AdminHandler) HandleAdminCustomPriceInput(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID
	text := c.Message().Text

	// Валидируем цену
	priceRegex := regexp.MustCompile(`^[\d.]+$`)
	if !priceRegex.MatchString(text) {
		return c.Send("❌ Пожалуйста, введите корректную цену")
	}

	tempData := ah.stateManager.GetTempData(userID)
	parts := strings.Split(tempData, "_")
	if len(parts) != 2 {
		return c.Edit("❌ Ошибка")
	}

	customOrderID, _ := strconv.Atoi(parts[1])
	price, _ := strconv.ParseFloat(text, 64)

	// Получаем user_id из custom_orders
	row := ah.db.QueryRow(ctx, "SELECT user_id FROM custom_orders WHERE id = $1", customOrderID)
	var customerUserID int64
	if err := row.Scan(&customerUserID); err != nil {
		return c.Edit("❌ Заказ не найден")
	}

	// Обновляем кастомный букет
	_, err := ah.db.Exec(ctx,
		`UPDATE custom_orders SET admin_price = $1, status = $2 WHERE id = $3`,
		price, models.CustomOrderStatusAccepted, customOrderID)
	if err != nil {
		log.Printf("❌ Ошибка обновления кастомного букета: %v\n", err)
		return c.Edit("❌ Ошибка при обновлении")
	}

	// Уведомляем пользователя
	user := &telebot.User{ID: customerUserID}
	msg := fmt.Sprintf(
		"🌸 <b>Ваш букет принят!</b>\n\n"+
			"💰 <b>Стоимость:</b> %g тг\n\n"+
			"Для оплаты нажмите на кнопку ниже.",
		price)

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			telebot.Btn{Text: "💳 Оплатить", Unique: fmt.Sprintf("pay_custom_%d", customOrderID)},
		),
	)

	if _, err := ah.bot.Send(user, msg, &telebot.SendOptions{ParseMode: telebot.ModeHTML}, menu); err != nil {
		log.Printf("❌ Ошибка отправки уведомления: %v\n", err)
	}

	ah.stateManager.ResetState(userID)
	log.Printf("✅ Кастомный букет #%d принят с ценой %g\n", customOrderID, price)

	return c.Edit(fmt.Sprintf("✅ Букет принят с ценой %g тг", price))
}

// HandleAdminCustomReject отклоняет кастомный букет
func (ah *AdminHandler) HandleAdminCustomReject(c telebot.Context, customOrderID int) error {
	ctx := context.Background()

	row := ah.db.QueryRow(ctx, "SELECT user_id FROM custom_orders WHERE id = $1", customOrderID)
	var customerUserID int64
	if err := row.Scan(&customerUserID); err != nil {
		return c.Edit("❌ Заказ не найден")
	}

	// Обновляем статус
	_, err := ah.db.Exec(ctx,
		`UPDATE custom_orders SET status = $1 WHERE id = $2`,
		models.CustomOrderStatusRejected, customOrderID)
	if err != nil {
		return c.Edit("❌ Ошибка обновления")
	}

	// Уведомляем пользователя
	user := &telebot.User{ID: customerUserID}
	msg := "😔 К сожалению, мы не сможем выполнить ваш запрос. Спасибо за интерес к нам! 🌸"
	if _, err := ah.bot.Send(user, msg); err != nil {
		log.Printf("❌ Ошибка отправки уведомления: %v\n", err)
	}

	return c.Edit("✅ Заказ отклонён")
}

// ========== НАСТРОЙКИ ==========

// HandleAdminSettings показывает меню настроек
func (ah *AdminHandler) HandleAdminSettings(c telebot.Context) error {
	ctx := context.Background()

	// Получаем текущие настройки
	row := ah.db.QueryRow(ctx,
		`SELECT shop_name, address, support_user_id, kaspi_link, about_channel_link FROM shop_settings LIMIT 1`)

	var shopName, address, kaspiLink, aboutChannelLink string
	var supportID *int64

	if err := row.Scan(&shopName, &address, &supportID, &kaspiLink, &aboutChannelLink); err != nil {
		return c.Edit("❌ Ошибка при загрузке настроек")
	}

	// Получаем support_user_id из настроек или используем пустую строку
	supportIDStr := "Не установлен"
	if supportID != nil {
		supportIDStr = fmt.Sprintf("%d", *supportID)
	}

	msg := fmt.Sprintf(
		"⚙️ <b>Текущие настройки:</b>\n\n"+
			"🏪 <b>Название:</b> %s\n"+
			"📍 <b>Адрес:</b> %s\n"+
			"👤 <b>ID поддержки:</b> %s\n"+
			"💳 <b>Kaspi Link:</b> %s\n"+
			"📢 <b>Канал:</b> %s",
		shopName, address, supportIDStr, kaspiLink, aboutChannelLink)

	menu := &telebot.ReplyMarkup{}
	menu.Inline(
		menu.Row(
			telebot.Btn{Text: "🏪 Название", Unique: "admin_set_name"},
		),
		menu.Row(
			telebot.Btn{Text: "📍 Адрес", Unique: "admin_set_address"},
		),
		menu.Row(
			telebot.Btn{Text: "👤 ID поддержки", Unique: "admin_set_support"},
		),
		menu.Row(
			telebot.Btn{Text: "💳 Kaspi Link", Unique: "admin_set_kaspi"},
		),
		menu.Row(
			telebot.Btn{Text: "📢 Канал", Unique: "admin_set_channel"},
		),
	)

	return c.Edit(msg, menu)
}

// HandleAdminSetName начинает изменение названия
func (ah *AdminHandler) HandleAdminSetName(c telebot.Context) error {
	userID := c.Sender().ID
	ah.stateManager.SetState(userID, models.StateAdminSetShopName)

	menu := &telebot.ReplyMarkup{ForceReply: true}
	return c.Edit("🏪 Введите новое название магазина:", menu)
}

// HandleAdminSetNameInput обрабатывает новое название
func (ah *AdminHandler) HandleAdminSetNameInput(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID
	text := c.Message().Text

	if text == "" || len(text) < 2 {
		return c.Send("❌ Пожалуйста, введите корректное название")
	}

	_, err := ah.db.Exec(ctx,
		`UPDATE shop_settings SET shop_name = $1`, text)
	if err != nil {
		return c.Send("❌ Ошибка обновления")
	}

	ah.stateManager.ResetState(userID)
	return c.Send(fmt.Sprintf("✅ Название изменено на: %s", text))
}

// HandleAdminSetAddress начинает изменение адреса
func (ah *AdminHandler) HandleAdminSetAddress(c telebot.Context) error {
	userID := c.Sender().ID
	ah.stateManager.SetState(userID, models.StateAdminSetAddress)

	menu := &telebot.ReplyMarkup{ForceReply: true}
	return c.Edit("📍 Введите новый адрес магазина:", menu)
}

// HandleAdminSetAddressInput обрабатывает новый адрес
func (ah *AdminHandler) HandleAdminSetAddressInput(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID
	text := c.Message().Text

	if text == "" || len(text) < 5 {
		return c.Send("❌ Пожалуйста, введите полный адрес")
	}

	_, err := ah.db.Exec(ctx,
		`UPDATE shop_settings SET address = $1`, text)
	if err != nil {
		return c.Send("❌ Ошибка обновления")
	}

	ah.stateManager.ResetState(userID)
	return c.Send(fmt.Sprintf("✅ Адрес изменено на: %s", text))
}

// HandleAdminSetSupport начинает изменение ID поддержки
func (ah *AdminHandler) HandleAdminSetSupport(c telebot.Context) error {
	userID := c.Sender().ID
	ah.stateManager.SetState(userID, models.StateAdminSetSupportID)

	menu := &telebot.ReplyMarkup{ForceReply: true}
	return c.Send("👤 Введите Telegram ID пользователя поддержки:", menu)
}

// HandleAdminSetSupportInput обрабатывает новый ID поддержки
func (ah *AdminHandler) HandleAdminSetSupportInput(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID
	text := c.Message().Text

	supportID, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return c.Send("❌ Пожалуйста, введите корректный Telegram ID")
	}

	_, err = ah.db.Exec(ctx,
		`UPDATE shop_settings SET support_user_id = $1`, supportID)
	if err != nil {
		return c.Send("❌ Ошибка обновления")
	}

	ah.stateManager.ResetState(userID)
	return c.Send(fmt.Sprintf("✅ ID поддержки изменено на: %d", supportID))
}

// HandleAdminSetKaspi начинает изменение Kaspi ссылки
func (ah *AdminHandler) HandleAdminSetKaspi(c telebot.Context) error {
	userID := c.Sender().ID
	ah.stateManager.SetState(userID, models.StateAdminSetKaspiLink)

	menu := &telebot.ReplyMarkup{ForceReply: true}
	return c.Send("💳 Введите ссылку на оплату магазина (Kaspi):", menu)
}

// HandleAdminSetKaspiInput обрабатывает новую Kaspi ссылку
func (ah *AdminHandler) HandleAdminSetKaspiInput(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID
	text := c.Message().Text

	if text == "" || len(text) < 5 {
		return c.Send("❌ Пожалуйста, введите корректную ссылку")
	}

	_, err := ah.db.Exec(ctx,
		`UPDATE shop_settings SET kaspi_link = $1`, text)
	if err != nil {
		return c.Send("❌ Ошибка обновления")
	}

	ah.stateManager.ResetState(userID)
	return c.Send("✅ Kaspi ссылка обновлена")
}

// HandleAdminSetChannel начинает изменение ссылки на канал
func (ah *AdminHandler) HandleAdminSetChannel(c telebot.Context) error {
	menu := &telebot.ReplyMarkup{ForceReply: true}
	return c.Edit("📢 Введите ссылку на канал 'О нас':", menu)
}

// HandleAdminSetChannelInput обрабатывает новую ссылку на канал
func (ah *AdminHandler) HandleAdminSetChannelInput(c telebot.Context) error {
	ctx := context.Background()
	userID := c.Sender().ID
	text := c.Message().Text

	if text == "" || len(text) < 5 {
		return c.Edit("❌ Пожалуйста, введите корректную ссылку")
	}

	_, err := ah.db.Exec(ctx,
		`UPDATE shop_settings SET about_channel_link = $1`, text)
	if err != nil {
		return c.Edit("❌ Ошибка обновления")
	}

	ah.stateManager.ResetState(userID)
	return c.Edit("✅ Ссылка на канал обновлена")
}

// ========== СТАТИСТИКА ==========

// HandleAdminStats показывает статистику
func (ah *AdminHandler) HandleAdminStats(c telebot.Context) error {
	ctx := context.Background()

	// Всего заказов
	totalRow := ah.db.QueryRow(ctx, "SELECT COUNT(*) FROM orders")
	var total int
	totalRow.Scan(&total)

	// Выполнено
	completeRow := ah.db.QueryRow(ctx,
		"SELECT COUNT(*) FROM orders WHERE status = $1", models.OrderStatusCompleted)
	var completed int
	completeRow.Scan(&completed)

	// Отменено
	cancelledRow := ah.db.QueryRow(ctx,
		"SELECT COUNT(*) FROM orders WHERE status = $1", models.OrderStatusCancelled)
	var cancelled int
	cancelledRow.Scan(&cancelled)

	// Активных
	activeRow := ah.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM orders WHERE status != $1 AND status != $2`,
		models.OrderStatusCompleted, models.OrderStatusCancelled)
	var active int
	activeRow.Scan(&active)

	// Сумма выполненных
	sumRow := ah.db.QueryRow(ctx,
		"SELECT COALESCE(SUM(amount), 0) FROM orders WHERE status = $1",
		models.OrderStatusCompleted)
	var totalSum float64
	sumRow.Scan(&totalSum)

	msg := fmt.Sprintf(
		"📊 <b>Статистика:</b>\n\n"+
			"📦 <b>Всего заказов:</b> %d\n"+
			"✅ <b>Выполнено:</b> %d\n"+
			"❌ <b>Отменено:</b> %d\n"+
			"🟡 <b>Активных:</b> %d\n"+
			"💰 <b>Выручка:</b> %g тг",
		total, completed, cancelled, active, totalSum)

	return c.Edit(msg, &telebot.SendOptions{ParseMode: telebot.ModeHTML})
}
