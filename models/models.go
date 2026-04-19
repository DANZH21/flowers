package models

import "time"

// State - состояние пользователя в FSM
type State int

const (
	StateNone State = iota
	// Запись на услугу
	StateAwaitingServiceSelection
	StateAwaitingDateSelection
	StateAwaitingTimeSelection
	StateAwaitingName
	StateAwaitingPhone
	StateAwaitingPaymentMethod
	StateAwaitingReceipt
	// Админ - управление услугами
	StateAdminAddServiceName
	StateAdminAddServiceDesc
	StateAdminAddServicePrice
	StateAdminAddServiceDuration
	StateAdminEditServiceName
	StateAdminEditServicePrice
	StateAdminEditServiceDuration
	// Админ - настройки
	StateAdminSetScheduleOpen
	StateAdminSetScheduleClose
	StateAdminSetReminderHours
	StateAdminSetPrepayPercent
	StateAdminSetSupportID
	StateAdminSetKaspiLink
	StateAdminSetChannelLink
	StateAdminSetSalonName
	StateAdminSetAddress
	StateAdminSetProtection
	StateAdminBroadcast
)

// AppointmentDraft - черновик записи на услугу
type AppointmentDraft struct {
	ServiceID       int
	AppointmentDate string // "2024-12-25" формат
	AppointmentTime string // "14:30" формат
	PaymentType     string // "kaspi" или "cash"
	Name            string
	Phone           string
	Amount          float64
}

// UserSession - сессия пользователя с его состоянием
type UserSession struct {
	State            State
	AppointmentDraft AppointmentDraft
	TempData         map[string]interface{}
	MessageIDs       []int // ID сообщений для удаления
	FullName         string
	Phone            string
	IsAdmin          bool
	LastOrderAttempt *time.Time
	CreatedAt        time.Time
}

// SalonSettings - настройки салона красоты
type SalonSettings struct {
	ID               int
	SalonName        string
	Address          string
	SupportUserID    *int64
	KaspiLink        string
	AboutChannelLink string
	ScheduleOpen     string // "10:00"
	ScheduleClose    string // "20:00"
	ReminderHours    int    // За сколько часов отправлять напоминание (1-24)
	PrepayPercent    int    // % предоплаты (0-100, 0 = отключена)
	CreatedAt        time.Time
}

// Service - услуга в салоне
type Service struct {
	ID          int
	Name        string
	Description string
	Price       float64
	DurationMin int // длительность в минутах
	IsAvailable bool
	CreatedAt   time.Time
}

// Appointment - запись на услугу
type Appointment struct {
	ID              int
	AppointmentNum  int
	UserID          int64
	ServiceID       int
	AppointmentTime time.Time // полная дата + время
	PaymentType     string    // "kaspi" или "cash"
	Amount          float64
	PrepayAmount    *float64
	CustomerName    string
	CustomerPhone   string
	Status          string // scheduled, confirmed, completed, cancelled, no_show
	ReceiptURL      *string
	ReceiptStatus   string // pending, confirmed, rejected (для Kaspi платежей)
	ReceiptDeadline *time.Time
	ReminderSent    bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// AppointmentStatus constants
const (
	AppointmentStatusScheduled = "scheduled"
	AppointmentStatusConfirmed = "confirmed"
	AppointmentStatusCompleted = "completed"
	AppointmentStatusCancelled = "cancelled"
	AppointmentStatusNoShow    = "no_show"
)

// ReceiptStatus constants
const (
	ReceiptStatusPending   = "pending"
	ReceiptStatusConfirmed = "confirmed"
	ReceiptStatusRejected  = "rejected"
)

// PaymentType constants
const (
	PaymentTypeKaspi = "kaspi"
	PaymentTypeCash  = "cash"
)
