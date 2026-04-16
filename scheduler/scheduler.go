package scheduler

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"flower-bot/db"
	"flower-bot/models"

	"gopkg.in/telebot.v3"
)

// Scheduler управляет таймерами для зарезервирования, чеков и антиспама
type Scheduler struct {
	timers map[string]*time.Timer
	mu     sync.Mutex
	db     *db.Database
	bot    *telebot.Bot
}

// NewScheduler создает новый планировщик
func NewScheduler(database *db.Database, bot *telebot.Bot) *Scheduler {
	return &Scheduler{
		timers: make(map[string]*time.Timer),
		db:     database,
		bot:    bot,
	}
}

// ScheduleReservationExpiry расписывает снятие резерва через 30 минут
func (s *Scheduler) ScheduleReservationExpiry(bouquetID int, userID int64, duration time.Duration) {
	key := fmt.Sprintf("reserve_%d", bouquetID)

	// Отменяем предыдущий таймер если есть
	s.CancelTimer(key)

	s.mu.Lock()
	s.timers[key] = time.AfterFunc(duration, func() {
		ctx := context.Background()

		// Снимаем резерв
		_, err := s.db.Exec(ctx,
			`UPDATE bouquets SET reserved_by = NULL, reserved_until = NULL WHERE id = $1`,
			bouquetID)
		if err != nil {
			log.Printf("❌ Ошибка снятия резерва букета %d: %v\n", bouquetID, err)
			return
		}

		// Уведомляем пользователя
		user := &telebot.User{ID: userID}
		msg := "⏳ Время резерва букета вышло. Букет возвращён в каталог."
		if _, err := s.bot.Send(user, msg); err != nil {
			log.Printf("❌ Ошибка отправки сообщения пользователю %d: %v\n", userID, err)
		}

		log.Printf("✅ Резерв букета %d снят\n", bouquetID)

		// Удаляем таймер из памяти
		s.mu.Lock()
		delete(s.timers, key)
		s.mu.Unlock()
	})
	s.mu.Unlock()

	log.Printf("⏲️ Расписан таймер резерва на букет %d на %v\n", bouquetID, duration)
}

// ScheduleReceiptDeadline расписывает дедлайн на получение чека (30 минут)
func (s *Scheduler) ScheduleReceiptDeadline(orderID int, userID int64, duration time.Duration) {
	key := fmt.Sprintf("receipt_%d", orderID)

	// Отменяем предыдущий таймер если есть
	s.CancelTimer(key)

	s.mu.Lock()
	s.timers[key] = time.AfterFunc(duration, func() {
		ctx := context.Background()

		// Получаем информацию о заказе
		row := s.db.QueryRow(ctx,
			`SELECT bouquet_id, amount FROM orders WHERE id = $1`,
			orderID)

		var bouquetID *int
		var amount float64
		if err := row.Scan(&bouquetID, &amount); err != nil {
			log.Printf("❌ Ошибка получения заказа %d: %v\n", orderID, err)
			return
		}

		// Отменяем заказ
		_, err := s.db.Exec(ctx,
			`UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2`,
			models.OrderStatusCancelled, orderID)
		if err != nil {
			log.Printf("❌ Ошибка отмены заказа %d: %v\n", orderID, err)
			return
		}

		// Возвращаем букет в каталог если был резерв
		if bouquetID != nil && *bouquetID > 0 {
			_, err := s.db.Exec(ctx,
				`UPDATE bouquets SET reserved_by = NULL, reserved_until = NULL WHERE id = $1`,
				*bouquetID)
			if err != nil {
				log.Printf("❌ Ошибка возврата букета %d: %v\n", *bouquetID, err)
			}
		}

		// Уведомляем пользователя
		user := &telebot.User{ID: userID}
		msg := fmt.Sprintf("❌ Заказ #%d автоматически отменён: время на отправку чека вышло (30 минут).", orderID)
		if _, err := s.bot.Send(user, msg); err != nil {
			log.Printf("❌ Ошибка отправки сообщения пользователю %d: %v\n", userID, err)
		}

		log.Printf("✅ Заказ %d отменён по истечению времени чека\n", orderID)

		// Удаляем таймер из памяти
		s.mu.Lock()
		delete(s.timers, key)
		s.mu.Unlock()
	})
	s.mu.Unlock()

	log.Printf("⏲️ Расписан дедлайн на чек для заказа %d на %v\n", orderID, duration)
}

// CancelTimer отменяет таймер по ключу
func (s *Scheduler) CancelTimer(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if timer, exists := s.timers[key]; exists {
		timer.Stop()
		delete(s.timers, key)
		log.Printf("✅ Таймер %s отменён\n", key)
	}
}

// CancelOrderTimer отменяет все таймеры связанные с заказом
func (s *Scheduler) CancelOrderTimer(orderID int) {
	s.CancelTimer(fmt.Sprintf("receipt_%d", orderID))
}

// CancelBouquetTimer отменяет таймер резерва букета
func (s *Scheduler) CancelBouquetTimer(bouquetID int) {
	s.CancelTimer(fmt.Sprintf("reserve_%d", bouquetID))
}

// Shutdown останавливает все таймеры
func (s *Scheduler) Shutdown() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, timer := range s.timers {
		timer.Stop()
		delete(s.timers, key)
	}
	log.Println("✅ Все таймеры завершены")
}
