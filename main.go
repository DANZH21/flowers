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
	"flower-bot/scheduler"

	"gopkg.in/telebot.v3"
)

func main() {
	// Загружаем конфиг
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("❌ Ошибка загрузки конфига: %v\n", err)
	}

	// Проверяем обязательные переменные
	if cfg.TelegramBotToken == "" {
		log.Fatalf("❌ TELEGRAM_BOT_TOKEN не установлен")
	}

	if cfg.DatabaseURL == "" {
		log.Fatalf("❌ DATABASE_URL не установлен")
	}

	log.Println("🚀 Запуск Flower Bot...")

	log.Println("🌸 ===== FLOWER BOT STARTING =====")

	// Подключаемся к БД (миграции запугаются автоматически с конфигом для инициализации)
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
	adminHandler := handlers.NewAdminHandler(database, bot, stateManager, sch, cfg.AdminIDs)
	log.Println("✅ [ОБРАБОТЧИКИ] Обработчики созданы")

	// ========== КОМАНДЫ ==========

	// /start
	log.Println("📝 [РЕГИСТРАЦИЯ] Регистрирую команду /start")
	bot.Handle("/start", func(c telebot.Context) error {
		return clientHandler.HandleStart(c)
	})

	// /admin
	bot.Handle("/admin", func(c telebot.Context) error {
		return adminHandler.HandleAdminMenu(c)
	})

	// ========== КЛИЕНТ - ТЕКСТОВЫЕ СООБЩЕНИЯ ==========

	// Обработчик для текстовых сообщений с FSM
	bot.Handle(telebot.OnText, func(c telebot.Context) error {
		userID := c.Sender().ID
		state := stateManager.GetState(userID)
		text := c.Message().Text

		log.Printf("💬 [TEXT] Пользователь %d отправил текст (состояние %d): %s\n", userID, state, text)

		// FSM обработчики
		switch state {
		case StateAwaitingName:
			return clientHandler.HandleNameInput(c)
		case StateAwaitingPhone:
			return clientHandler.HandlePhoneInput(c)
		case StateAwaitingDate:
			return clientHandler.HandleDateInput(c)
		case StateAwaitingAddress:
			return clientHandler.HandleAddressInput(c)
		case StateAwaitingReceipt:
			return clientHandler.HandleReceiptInput(c)
		case StateAwaitingCustomBouquet:
			return clientHandler.HandleCustomBouquetInput(c)
		case StateAdminSetPrice:
			if adminHandler.IsAdmin(userID) {
				return adminHandler.HandleAdminCustomPriceInput(c)
			}
		case StateAdminSetShopName:
			if adminHandler.IsAdmin(userID) {
				return adminHandler.HandleAdminSetNameInput(c)
			}
		case StateAdminSetAddress:
			if adminHandler.IsAdmin(userID) {
				return adminHandler.HandleAdminSetAddressInput(c)
			}
		case StateAdminSetSupportID:
			if adminHandler.IsAdmin(userID) {
				return adminHandler.HandleAdminSetSupportInput(c)
			}
		case StateAdminSetKaspiLink:
			if adminHandler.IsAdmin(userID) {
				return adminHandler.HandleAdminSetKaspiInput(c)
			}
		case StateAdminAddBouquetName:
			if adminHandler.IsAdmin(userID) {
				return adminHandler.HandleAdminBouquetNameInput(c)
			}
		case StateAdminAddBouquetDesc:
			if adminHandler.IsAdmin(userID) {
				return adminHandler.HandleAdminBouquetDescInput(c)
			}
		case StateAdminAddBouquetPrice:
			if adminHandler.IsAdmin(userID) {
				return adminHandler.HandleAdminBouquetPriceInput(c)
			}
		case StateAdminAddBouquetQty:
			if adminHandler.IsAdmin(userID) {
				return adminHandler.HandleAdminBouquetQtyInput(c)
			}
		case StateAdminAddBouquetPhoto:
			if adminHandler.IsAdmin(userID) {
				return adminHandler.HandleAdminBouquetPhotoInput(c)
			}
		}

		return nil
	})

	// Обработчик для фото (чеков)
	bot.Handle(telebot.OnPhoto, func(c telebot.Context) error {
		userID := c.Sender().ID
		state := stateManager.GetState(userID)

		if state == StateAwaitingReceipt {
			return clientHandler.HandleReceiptInput(c)
		}

		if state == StateAdminAddBouquetPhoto && adminHandler.IsAdmin(userID) {
			return adminHandler.HandleAdminBouquetPhotoInput(c)
		}

		return nil
	})

	// Обработчик для документов
	bot.Handle(telebot.OnDocument, func(c telebot.Context) error {
		userID := c.Sender().ID
		state := stateManager.GetState(userID)

		if state == StateAwaitingReceipt {
			return clientHandler.HandleReceiptInput(c)
		}

		return nil
	})

	// ========== CALLBACK ЗАПРОСЫ (Inline кнопки) ==========

	bot.Handle(telebot.OnCallback, func(c telebot.Context) error {
		data := strings.TrimSpace(c.Callback().Data) // ← TRIM WHITESPACE!
		userID := c.Sender().ID
		queryID := c.Callback().ID

		log.Printf("\n" + strings.Repeat("=", 80))
		log.Printf("🔘 [CALLBACK START] QueryID: %s | Пользователь: %d | Данные: '%s'\n", queryID, userID, data)
		log.Printf("   Время: %v\n", time.Now().Format("15:04:05"))
		log.Printf("   Username: @%s | FName: %s\n", c.Sender().Username, c.Sender().FirstName)
		log.Printf("   Длина data: %d | Hex: %X\n", len(data), []byte(data))

		// Ответ на callback (убирает "часы загрузки")
		respondErr := c.Respond()
		if respondErr != nil {
			log.Printf("❌ [CALLBACK] Ошибка ответа на callback: %v\n", respondErr)
		} else {
			log.Printf("✅ [CALLBACK] Ответ отправлен (loading dismissed)\n")
		}

		// ===== ГЛАВНОЕ МЕНЮ КНОПКИ =====
		switch data {
		case "catalog":
			log.Printf("📦 [МАРШРУТ] -> КАТАЛОГ\n")
			log.Printf("   Вызываю: clientHandler.HandleCatalog()\n")
			return clientHandler.HandleCatalog(c)

		case "about":
			log.Printf("ℹ️ [МАРШРУТ] -> О НАС\n")
			log.Printf("   Вызываю: clientHandler.HandleAbout()\n")
			return clientHandler.HandleAbout(c)

		case "address":
			log.Printf("📍 [МАРШРУТ] -> АДРЕС\n")
			log.Printf("   Вызываю: clientHandler.HandleAddress()\n")
			return clientHandler.HandleAddress(c)

		case "support":
			log.Printf("💬 [МАРШРУТ] -> ПОДДЕРЖКА\n")
			log.Printf("   Вызываю: clientHandler.HandleSupport()\n")
			return clientHandler.HandleSupport(c)

		case "my_orders":
			log.Printf("📋 [МАРШРУТ] -> МОИ ЗАКАЗЫ\n")
			log.Printf("   Вызываю: clientHandler.HandleMyOrders()\n")
			return clientHandler.HandleMyOrders(c)

		case "custom_bouquet":
			log.Printf("🎨 [МАРШРУТ] -> СВОЙ БУКЕТ\n")
			log.Printf("   Вызываю: clientHandler.HandleCustomBouquet()\n")
			return clientHandler.HandleCustomBouquet(c)

		case "main_menu":
			log.Printf("🏠 [МАРШРУТ] -> ГЛАВНОЕ МЕНЮ\n")
			log.Printf("   Вызываю: clientHandler.HandleStart()\n")
			return clientHandler.HandleStart(c)
		}

		// ===== ПАГИНАЦИЯ КАТАЛОГА =====
		if isPageBtn, page := parseCallback(data, "catalog_page_"); isPageBtn {
			log.Printf("📄 [МАРШРУТ] -> СТРАНИЦА КАТАЛОГА %d\n", page)
			log.Printf("   Вызываю: clientHandler.HandleCatalogPage()\n")
			return clientHandler.HandleCatalogPage(c, page)
		}

		// ===== ЗАКАЗ БУКЕТА =====
		if isOrderBtn, bouquetID := parseCallback(data, "order_"); isOrderBtn {
			log.Printf("🛒 [МАРШРУТ] -> ЗАКАЗАТЬ БУКЕТ\n")
			log.Printf("   BouquetID: %d\n", bouquetID)
			log.Printf("   Вызываю: clientHandler.HandleOrderStart()\n")
			return clientHandler.HandleOrderStart(c, bouquetID)
		}

		// ===== ЗАРЕЗЕРВИРОВАН =====
		if isReservedBtn, _ := parseCallback(data, "reserved_"); isReservedBtn {
			log.Printf("⏳ [МАРШРУТ] -> БУКЕТ ЗАРЕЗЕРВИРОВАН\n")
			return c.Respond(&telebot.CallbackResponse{Text: "⏳ Этот букет зарезервирован", ShowAlert: false})
		}

		// ===== НЕТ В НАЛИЧИИ =====
		if isUnavailBtn, _ := parseCallback(data, "unavail_"); isUnavailBtn {
			log.Printf("❌ [МАРШРУТ] -> БУКЕТ НЕ В НАЛИЧИИ\n")
			return c.Respond(&telebot.CallbackResponse{Text: "❌ Букет закончился", ShowAlert: false})
		}

		// ===== ПОДТВЕРЖДЕНИЕ ЗАКАЗА ИЗ ДЕТАЛЕЙ =====
		if isConfirmOrder, bouquetID := parseCallback(data, "orderconfirm_"); isConfirmOrder {
			log.Printf("💳 [МАРШРУТ] -> ПОДТВЕРЖДЕНИЕ ЗАКАЗА ИЗ ДЕТАЛЕЙ\n")
			log.Printf("   BouquetID: %d\n", bouquetID)
			log.Printf("   Вызываю: clientHandler.HandleOrderConfirmFromDetails()\n")
			return clientHandler.HandleOrderConfirmFromDetails(c, bouquetID)
		}

		// ===== НАЗАД В КАТАЛОГ =====
		if data == "back_to_catalog" {
			log.Printf("📦 [МАРШРУТ] -> НАЗАД В КАТАЛОГ\n")
			log.Printf("   Вызываю: clientHandler.HandleCatalog()\n")
			return clientHandler.HandleCatalog(c)
		}

		// ===== ВЫБОР СПОСОБА ДОСТАВКИ (ДОСТАВКА vs САМОВЫВОЗ) =====
		if isDeliveryChoice, deliveryType := parseCallbackDeliveryType(data, "delivery_choice_"); isDeliveryChoice {
			log.Printf("🚚 [МАРШРУТ] -> ВЫБОР СПОСОБА ДОСТАВКИ\n")
			log.Printf("   Способ: %s\n", deliveryType)
			log.Printf("   Вызываю: clientHandler.HandleDeliveryChoiceSelect()\n")
			return clientHandler.HandleDeliveryChoiceSelect(c, deliveryType)
		}

		// ===== ВЫБОР ДАТЫ ДОСТАВКИ =====
		if isDateBtn, dateChoice := parseCallbackDeliveryType(data, "delivery_date_"); isDateBtn {
			log.Printf("📅 [МАРШРУТ] -> ВЫБОР ДАТЫ ДОСТАВКИ\n")
			log.Printf("   Дата: %s\n", dateChoice)
			log.Printf("   Вызываю: clientHandler.HandleDeliveryDateSelect()\n")
			return clientHandler.HandleDeliveryDateSelect(c, dateChoice)
		}

		// ===== ВЫБОР ДОСТАВКИ (УСТАРЕВШИЙ - ОСТАВИТЬ ДЛЯ СОВМЕСТИМОСТИ) =====
		if isDeliveryBtn, deliveryType := parseCallbackDeliveryType(data, "delivery_type_"); isDeliveryBtn {
			log.Printf("🚚 [МАРШРУТ] -> ДОСТАВКА (СТАРЫЙ МАРШРУТ)\n")
			log.Printf("   Тип: %s\n", deliveryType)
			log.Printf("   Вызываю: clientHandler.HandleDeliveryTypeSelect()\n")
			return clientHandler.HandleDeliveryTypeSelect(c, deliveryType)
		}

		// ===== ВЫБОР ОПЛАТЫ =====
		if isPaymentBtn, paymentType := parseCallbackPaymentType(data, "payment_type_"); isPaymentBtn {
			log.Printf("💳 [МАРШРУТ] -> ОПЛАТА\n")
			log.Printf("   Тип: %s\n", paymentType)
			log.Printf("   Вызываю: clientHandler.HandlePaymentTypeSelect()\n")
			return clientHandler.HandlePaymentTypeSelect(c, paymentType)
		}

		// ===== ПОДТВЕРЖДЕНИЕ ЗАКАЗА =====
		if isConfirmBtn, _ := parseCallback(data, "confirm_order_"); isConfirmBtn {
			log.Printf("✅ [МАРШРУТ] -> ПОДТВЕРЖДЕНИЕ ЗАКАЗА\n")
			log.Printf("   Вызываю: clientHandler.HandleConfirmOrder()\n")
			return clientHandler.HandleConfirmOrder(c)
		}

		// ===== ОТМЕНА ЗАКАЗА =====
		if isCancelBtn, _ := parseCallback(data, "cancel_order_"); isCancelBtn {
			log.Printf("❌ [МАРШРУТ] -> ОТМЕНА ЗАКАЗА\n")
			log.Printf("   Вызываю: clientHandler.HandleCancelOrder()\n")
			return clientHandler.HandleCancelOrder(c)
		}

		// ===== АДМИНИСТРАТОРСКИЕ КНОПКИ =====
		log.Printf("🔐 [ПРОВЕРКА] Проверяю админ статус...\n")
		err := handleAdminCallbacks(c, adminHandler, clientHandler, database, stateManager, sch, data, userID)
		if err != nil {
			log.Printf("⚠️ [CALLBACK] Результат админ обработчика: %v\n", err)
		}
		log.Printf("=" + strings.Repeat("=", 79) + "\n\n")
		return err
	})

	// ========== GRACEFUL SHUTDOWN ==========

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Запускаем бота в отдельной горутине
	go func() {
		log.Println("✅ Бот запущен и готов к получению сообщений")
		bot.Start()
	}()

	// Ждем сигнала завершения
	<-ctx.Done()

	log.Println("\n🛑 Получен сигнал завершения, закрываем бота...")
	bot.Stop()
	sch.Shutdown()

	log.Println("✅ Бот остановлен")
}

// ========== ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ ==========

// parseCallback парсит callback с ID
func parseCallback(data, prefix string) (bool, int) {
	data = strings.TrimSpace(data)
	if len(data) > len(prefix) && strings.HasPrefix(data, prefix) {
		idStr := data[len(prefix):]
		// Ищем цифры в начале
		endIdx := 0
		for i, char := range idStr {
			if char < '0' || char > '9' {
				endIdx = i
				break
			}
			if i == len(idStr)-1 {
				endIdx = len(idStr)
			}
		}
		if endIdx > 0 {
			id := 0
			fmt.Sscanf(idStr[:endIdx], "%d", &id)
			return true, id
		}
	}
	return false, 0
}

// parseCallbackDeliveryType парсит delivery type из callback
func parseCallbackDeliveryType(data, prefix string) (bool, string) {
	data = strings.TrimSpace(data)
	// Формат: "delivery_type_12345_delivery" или "delivery_type_12345_pickup"
	if strings.HasPrefix(data, prefix) {
		parts := strings.Split(data, "_")
		if len(parts) >= 4 {
			return true, parts[len(parts)-1]
		}
	}
	return false, ""
}

// parseCallbackPaymentType парсит payment type из callback
func parseCallbackPaymentType(data, prefix string) (bool, string) {
	data = strings.TrimSpace(data)
	// Формат: "payment_type_12345_kaspi" или "payment_type_12345_cash"
	if strings.HasPrefix(data, prefix) {
		parts := strings.Split(data, "_")
		if len(parts) >= 4 {
			return true, parts[len(parts)-1]
		}
	}
	return false, ""
}

// handleAdminCallbacks обрабатывает все админ callbacks
func handleAdminCallbacks(c telebot.Context, ah *handlers.AdminHandler, ch *handlers.ClientHandler,
	database *db.Database, sm *handlers.StateManager, sch *scheduler.Scheduler, data string, userID int64) error {

	data = strings.TrimSpace(data) // ← TRIM WHITESPACE!

	if !ah.IsAdmin(userID) {
		log.Printf("⚠️ [БЕЗОПАСНОСТЬ] Попытка несанкционированного доступа (пользователь %d)\n", userID)
		return c.Respond(&telebot.CallbackResponse{Text: "❌ Доступ запрещён", ShowAlert: true})
	}

	log.Printf("🔐 [АДМИНИСТРАТОР] Админ %d нажал: '%s' (len=%d)\n", userID, data, len(data))

	// Admin меню
	if data == "admin_orders" {
		log.Printf("📋 [АДМИН] Администратор %d открыл список заказов\n", userID)
		return ah.HandleAdminOrders(c)
	}
	if data == "admin_catalog" {
		log.Printf("🌸 [АДМИН] Администратор %d открыл каталог\n", userID)
		return ah.HandleAdminCatalog(c)
	}
	if data == "admin_settings" {
		log.Printf("⚙️ [АДМИН] Администратор %d открыл настройки\n", userID)
		return ah.HandleAdminSettings(c)
	}
	if data == "admin_stats" {
		return ah.HandleAdminStats(c)
	}

	// Order callbacks
	if isOrderDetailBtn, orderID := parseCallback(data, "admin_order_detail_"); isOrderDetailBtn {
		return ah.HandleAdminOrderDetail(c, orderID)
	}
	if isConfirmBtn, orderID := parseCallback(data, "admin_confirm_order_"); isConfirmBtn {
		return ah.HandleAdminConfirmOrder(c, orderID)
	}
	if isConfirmYesBtn, orderID := parseCallback(data, "admin_confirm_yes_"); isConfirmYesBtn {
		return ah.HandleAdminConfirmOrderYes(c, orderID)
	}
	if isCancelBtn, orderID := parseCallback(data, "admin_cancel_order_"); isCancelBtn {
		return ah.HandleAdminCancelOrder(c, orderID)
	}
	if isCancelYesBtn, orderID := parseCallback(data, "admin_cancel_yes_"); isCancelYesBtn {
		return ah.HandleAdminCancelOrderYes(c, orderID)
	}
	if isDeliveringBtn, orderID := parseCallback(data, "admin_delivering_"); isDeliveringBtn {
		return ah.HandleAdminDelivering(c, orderID)
	}
	if isCompleteBtn, orderID := parseCallback(data, "admin_complete_"); isCompleteBtn {
		return ah.HandleAdminComplete(c, orderID)
	}

	// Payment callbacks
	if isAcceptBtn, orderID := parseCallback(data, "admin_payment_accept_"); isAcceptBtn {
		return ah.HandleAdminPaymentAccept(c, orderID)
	}
	if isAcceptYesBtn, orderID := parseCallback(data, "admin_accept_yes_"); isAcceptYesBtn {
		return ah.HandleAdminPaymentAcceptYes(c, orderID)
	}
	if isRejectBtn, orderID := parseCallback(data, "admin_payment_reject_"); isRejectBtn {
		return ah.HandleAdminPaymentReject(c, orderID)
	}
	if isRejectYesBtn, orderID := parseCallback(data, "admin_reject_yes_"); isRejectYesBtn {
		return ah.HandleAdminPaymentRejectYes(c, orderID)
	}

	// Catalog callbacks
	if data == "admin_add_bouquet" {
		return ah.HandleAdminAddBouquet(c)
	}
	if isBouquetDetailBtn, bouquetID := parseCallback(data, "admin_bouquet_detail_"); isBouquetDetailBtn {
		return ah.HandleAdminBouquetDetail(c, bouquetID)
	}
	if isToggleBtn, bouquetID := parseCallback(data, "admin_toggle_bouquet_"); isToggleBtn {
		return ah.HandleAdminToggleBouquet(c, bouquetID)
	}
	if isDeleteBtn, bouquetID := parseCallback(data, "admin_delete_bouquet_"); isDeleteBtn {
		return ah.HandleAdminDeleteBouquet(c, bouquetID)
	}

	// Custom order callbacks
	if isCustomAcceptBtn, customID := parseCallback(data, "admin_custom_accept_"); isCustomAcceptBtn {
		return ah.HandleAdminCustomAccept(c, customID)
	}
	if isCustomRejectBtn, customID := parseCallback(data, "admin_custom_reject_"); isCustomRejectBtn {
		return ah.HandleAdminCustomReject(c, customID)
	}

	// Settings callbacks
	if data == "admin_set_name" {
		return ah.HandleAdminSetName(c)
	}
	if data == "admin_set_address" {
		return ah.HandleAdminSetAddress(c)
	}
	if data == "admin_set_support" {
		return ah.HandleAdminSetSupport(c)
	}
	if data == "admin_set_kaspi" {
		return ah.HandleAdminSetKaspi(c)
	}
	if data == "admin_set_channel" {
		return ah.HandleAdminSetChannel(c)
	}

	// Cancel confirm callbacks
	if _, orderID := parseCallback(data, "admin_cancel_confirm_"); orderID > 0 {
		log.Printf("⏱️ [АДМИН] Администратор %d отменил подтверждение\n", userID)
		return c.Edit("❌ Отменено")
	}

	log.Printf("⚠️ [CALLBACK] ❌ НЕИЗВЕСТНЫЙ callback для администратора %d\n", userID)
	log.Printf("   data='%s' | len=%d | hex=%X\n", data, len(data), []byte(data))
	log.Printf("   💡 ПОДСКАЗКА: callback должен был быть обработан выше\n")
	log.Printf("   (это может быть глобальная кнопка типа catalog/about/address)\n")
	return nil
}

// Constants для состояний FSM (переэкспортированы сюда для удобства)
const (
	StateNone = iota
	StateAwaitingName
	StateAwaitingPhone
	StateAwaitingDate
	StateAwaitingAddress
	StateAwaitingReceipt
	StateAwaitingCustomBouquet
	StateAdminSetPrice
	StateAdminSetSupportID
	StateAdminSetKaspiLink
	StateAdminSetShopName
	StateAdminSetAddress
	StateAdminAddBouquetName
	StateAdminAddBouquetDesc
	StateAdminAddBouquetPrice
	StateAdminAddBouquetQty
	StateAdminAddBouquetPhoto
)
