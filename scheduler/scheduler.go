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

// Scheduler управляет таймерами для записей и напоминаний
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

// ScheduleAppointmentReminder расписывает напоминание о записи
func (s *Scheduler) ScheduleAppointmentReminder(appointmentID int, userID int64, appointmentTime time.Time) {
	key := fmt.Sprintf("reminder_%d", appointmentID)

	// Получаем настройки напоминания
	ctx := context.Background()
	salonSettings, err := s.db.GetSalonSettings(ctx)
	if err != nil {
		log.Printf("❌ Ошибка получения настроек: %v\n", err)
		return
	}

	reminderHours := salonSettings["reminder_hours"].(int)
	reminderTime := appointmentTime.Add(-time.Duration(reminderHours) * time.Hour)
	duration := time.Until(reminderTime)

	if duration <= 0 {
		// Запись уже прошла или время напоминания истекло
		return
	}

	// Отменяем предыдущий таймер если есть
	s.CancelTimer(key)

	s.mu.Lock()
	s.timers[key] = time.AfterFunc(duration, func() {
		ctx := context.Background()

		// Получаем информацию о записи
		row := s.db.QueryRow(ctx,
			`SELECT a.id, a.appointment_num, s.name, a.appointment_time, a.customer_name
			FROM appointments a
			JOIN services s ON a.service_id = s.id
			WHERE a.id = $1`, appointmentID)

		var id, appointmentNum int
		var serviceName, customerName string
		var apptTime time.Time

		if err := row.Scan(&id, &appointmentNum, &serviceName, &apptTime, &customerName); err != nil {
			log.Printf("❌ Ошибка получения записи: %v\n", err)
			return
		}

		// Отправляем напоминание
		user := &telebot.User{ID: userID}
		msg := fmt.Sprintf(
			"🔔 Напоминание о вашей записи!\n\n"+
				"#%d\n"+
				"💅 %s\n"+
				"📅 %s\n\n"+
				"Вы готовы к визиту? ✅",
			appointmentNum, serviceName, apptTime.Format("02 Jan 15:04"))

		if _, err := s.bot.Send(user, msg); err != nil {
			log.Printf("❌ Ошибка отправки напоминания пользователю %d: %v\n", userID, err)
		}

		// Обновляем флаг напоминания
		_, err := s.db.Exec(ctx,
			`UPDATE appointments SET reminder_sent = true WHERE id = $1`, appointmentID)
		if err != nil {
			log.Printf("❌ Ошибка обновления флага напоминания: %v\n", err)
		}

		log.Printf("✅ Напоминание отправлено для записи %d\n", appointmentID)

		// Удаляем таймер из памяти
		s.mu.Lock()
		delete(s.timers, key)
		s.mu.Unlock()
	})
	s.mu.Unlock()

	log.Printf("⏲️ Напоминание расписано для записи %d на %d часов до начала\n", appointmentID, reminderHours)
}

// ScheduleReceiptDeadline расписывает дедлайн на получение чека (30 минут)
func (s *Scheduler) ScheduleReceiptDeadline(appointmentID int, userID int64, duration time.Duration) {
	key := fmt.Sprintf("receipt_%d", appointmentID)

	// Отменяем предыдущий таймер если есть
	s.CancelTimer(key)

	s.mu.Lock()
	s.timers[key] = time.AfterFunc(duration, func() {
		ctx := context.Background()

		// Отменяем запись
		_, err := s.db.Exec(ctx,
			`UPDATE appointments SET status = $1, updated_at = NOW() WHERE id = $2`,
			models.AppointmentStatusCancelled, appointmentID)
		if err != nil {
			log.Printf("❌ Ошибка отмены записи %d: %v\n", appointmentID, err)
			return
		}

		// Уведомляем пользователя
		user := &telebot.User{ID: userID}
		msg := fmt.Sprintf("❌ Запись автоматически отменена: время на отправку чека вышло (30 минут).")
		if _, err := s.bot.Send(user, msg); err != nil {
			log.Printf("❌ Ошибка отправки сообщения пользователю %d: %v\n", userID, err)
		}

		log.Printf("✅ Запись %d отменена по истечению времени чека\n", appointmentID)

		// Удаляем таймер из памяти
		s.mu.Lock()
		delete(s.timers, key)
		s.mu.Unlock()
	})
	s.mu.Unlock()

	log.Printf("⏲️ Дедлайн на чек расписан для записи %d на %v\n", appointmentID, duration)
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

// CancelAppointmentTimers отменяет все таймеры связанные с записью
func (s *Scheduler) CancelAppointmentTimers(appointmentID int) {
	s.CancelTimer(fmt.Sprintf("receipt_%d", appointmentID))
	s.CancelTimer(fmt.Sprintf("reminder_%d", appointmentID))
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
