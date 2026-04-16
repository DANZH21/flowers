package models

import "time"

// State - состояние пользователя в FSM
type State int

const (
	StateNone State = iota
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

// OrderDraft - черновик заказа
type OrderDraft struct {
	BouquetID    int
	DeliveryType string // "delivery" или "pickup"
	DeliveryDate string // "2024-12-25" формат
	PaymentType  string // "kaspi" или "cash"
	Name         string
	Phone        string
	Address      string
	Amount       float64
}

// UserSession - сессия пользователя с его состоянием
type UserSession struct {
	State       State
	OrderDraft  OrderDraft
	CustomDraft string
	TempData    string
}

// User - пользователь в системе
type User struct {
	TelegramID       int64
	Username         string
	FullName         string
	Phone            string
	IsAdmin          bool
	LastOrderAttempt *time.Time
	CreatedAt        time.Time
}

// ShopSettings - настройки магазина
type ShopSettings struct {
	ID               int
	ShopName         string
	Address          string
	SupportUserID    *int64
	KaspiLink        string
	AboutChannelLink string
	CreatedAt        time.Time
}

// Bouquet - букет в каталоге
type Bouquet struct {
	ID            int
	Name          string
	Description   string
	Price         float64
	PhotoURLs     []string
	Quantity      int
	IsAvailable   bool
	ReservedUntil *time.Time
	ReservedBy    *int64
	CreatedAt     time.Time
}

// Order - заказ
type Order struct {
	ID              int
	OrderNumber     int
	UserID          int64
	BouquetID       *int
	CustomOrderID   *int
	DeliveryType    string // "delivery" или "pickup"
	PaymentType     string // "kaspi" или "cash"
	Amount          float64
	PrepayAmount    *float64
	CustomerName    string
	CustomerPhone   string
	DeliveryAddress *string
	Status          string // pending, confirmed, delivering, completed, cancelled
	ReceiptURL      *string
	ReceiptDeadline *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// CustomOrder - кастомный букет
type CustomOrder struct {
	ID          int
	UserID      int64
	Description string
	AdminPrice  *float64
	Status      string // pending, accepted, rejected, paid
	CreatedAt   time.Time
}

// OrderStatus constants
const (
	OrderStatusPending    = "pending"
	OrderStatusConfirmed  = "confirmed"
	OrderStatusDelivering = "delivering"
	OrderStatusCompleted  = "completed"
	OrderStatusCancelled  = "cancelled"
)

// CustomOrderStatus constants
const (
	CustomOrderStatusPending  = "pending"
	CustomOrderStatusAccepted = "accepted"
	CustomOrderStatusRejected = "rejected"
	CustomOrderStatusPaid     = "paid"
)

// DeliveryType constants
const (
	DeliveryTypeDelivery = "delivery"
	DeliveryTypePickup   = "pickup"
)

// PaymentType constants
const (
	PaymentTypeKaspi = "kaspi"
	PaymentTypeCash  = "cash"
)
