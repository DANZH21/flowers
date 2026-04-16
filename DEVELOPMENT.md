# 🛠️ Руководство по разработке Flower Bot

## Локальная разработка

### Предварительные требования

- Go 1.21+
- PostgreSQL 14+
- Docker & Docker Compose (опционально)
- Git

### Быстрый старт

```bash
# 1. Клонируем репозиторий
git clone <repo-url>
cd flower-bot

# 2. Создаем .env файл
cp .env.example .env

# 3. Заполняем значения
nano .env

# 4. Используя Docker Compose (рекомендуется)
docker-compose up -d

# Или локально:
# - Запускаем PostgreSQL
# - Выполняем: go run main.go
```

### Структура кода

```
handlers/
├── states.go      # StateManager - менеджер FSM состояний
├── client.go      # Все обработчики для клиентов
└── admin.go       # Все обработчики для администраторов

db/
├── db.go          # Подключение к БД через pgxpool
└── migrations.go  # Автоматические миграции

scheduler/
└── scheduler.go   # Таймеры (резерв, чеки, и т.д.)

config/
└── config.go      # Загрузка конфигурации из .env
```

## Добавление новой функции

### Пример 1: Добавить новый хэндлер для клиента

```go
// handlers/client.go

// Добавляем метод в ClientHandler
func (ch *ClientHandler) HandleMyNewFeature(c telebot.Context) error {
    userID := c.Sender().ID
    
    // Ваш код
    return c.Send("✅ Это работает!")
}
```

Затем в `main.go` подписываемся на кнопку/команду:

```go
// В обработчике текстовых сообщений
case "🆕 Новая функция":
    return clientHandler.HandleMyNewFeature(c)
```

### Пример 2: Добавить админ-функцию

```go
// handlers/admin.go

func (ah *AdminHandler) HandleAdminNewFeature(c telebot.Context) error {
    userID := c.Sender().ID
    
    if !ah.IsAdmin(userID) {
        return c.Send("❌ Доступ запрещён")
    }
    
    // Ваш код
    return c.Send("✅ Админ функция работает!")
}
```

Подписываемся в `handleAdminCallbacks` в `main.go`:

```go
if data == "admin_new_feature" {
    return ah.HandleAdminNewFeature(c)
}
```

### Пример 3: Добавить новое состояние FSM

```go
// models/models.go

const (
    // ... существующие состояния
    StateMyNewState models.State = iota
)
```

Затем в обработчике текста:

```go
// main.go обработчик OnText

case StateMyNewState:
    return clientHandler.HandleMyNewStateInput(c)
```

### Пример 4: Добавить таймер

```go
// scheduler/scheduler.go

func (s *Scheduler) ScheduleMyTimer(userID int64, duration time.Duration) {
    key := fmt.Sprintf("my_timer_%d", userID)
    
    s.CancelTimer(key)
    
    s.mu.Lock()
    s.timers[key] = time.AfterFunc(duration, func() {
        // Действие по истечении времени
        user := &telebot.User{ID: userID}
        s.bot.Send(user, "⏰ Время вышло!")
        
        s.mu.Lock()
        delete(s.timers, key)
        s.mu.Unlock()
    })
    s.mu.Unlock()
}
```

## Работа с базой данных

### Выполнить запрос SELECT с одной строкой

```go
ctx := context.Background()
row := ch.db.QueryRow(ctx, 
    "SELECT id, name FROM users WHERE telegram_id = $1", 
    userID)

var id int
var name string
if err := row.Scan(&id, &name); err != nil {
    log.Printf("❌ Ошибка: %v\n", err)
    return c.Send("❌ Ошибка")
}
```

### Выполнить запрос SELECT со множеством строк

```go
rows, err := ch.db.Query(ctx, 
    "SELECT id, name FROM users WHERE is_admin = $1", 
    true)
if err != nil {
    return c.Send("❌ Ошибка")
}
defer rows.Close()

for rows.Next() {
    var id int
    var name string
    if err := rows.Scan(&id, &name); err != nil {
        log.Printf("❌ Ошибка: %v\n", err)
        continue
    }
    // Обрабатываем строку
}
```

### Выполнить INSERT/UPDATE/DELETE

```go
_, err := ch.db.Exec(ctx,
    `UPDATE users SET full_name = $1 WHERE telegram_id = $2`,
    newName, userID)
if err != nil {
    log.Printf("❌ Ошибка: %v\n", err)
    return c.Send("❌ Ошибка")
}
```

### Получить ID вставленной записи

```go
var newID int
err := ch.db.QueryRow(ctx,
    `INSERT INTO users (name, email) VALUES ($1, $2) RETURNING id`,
    name, email).Scan(&newID)
if err != nil {
    return c.Send("❌ Ошибка")
}
```

## Состояния пользователя (FSM)

### Установить состояние

```go
stateManager.SetState(userID, models.StateAwaitingName)
```

### Получить состояние

```go
state := stateManager.GetState(userID)
if state == models.StateAwaitingName {
    // Ответить на ввод имени
}
```

### Сохранить временные данные

```go
stateManager.SetTempData(userID, "какие-то-данные")
tempData := stateManager.GetTempData(userID)
```

### Очистить сессию

```go
stateManager.ResetState(userID)
```

## Отправка сообщений

### Простое сообщение

```go
return c.Send("Привет! 👋")
```

### С HTML форматированием

```go
return c.Send(
    "<b>Жирный текст</b>\n<i>Курсив</i>", 
    &telebot.SendOptions{ParseMode: telebot.ModeHTML})
```

### С клавиатурой (ReplyKeyboard)

```go
menu := &telebot.ReplyMarkup{ResizeKeyboard: true}
menu.Reply(
    menu.Row(
        telebot.Btn{Text: "📦 Заказы"},
        telebot.Btn{Text: "🌸 Каталог"},
    ),
)
return c.Send("Выберите опцию:", menu)
```

### С inline кнопками (InlineKeyboard)

```go
menu := &telebot.InlineMarkup{}
menu.Inline(
    menu.Row(
        telebot.Btn{Text: "✅ Да", Unique: "confirm_yes"},
        telebot.Btn{Text: "❌ Нет", Unique: "confirm_no"},
    ),
)
return c.Send("Вы уверены?", menu)
```

### С фото

```go
photo := &telebot.Photo{File: telebot.FromURL("https://example.com/image.jpg")}
return c.Send(photo, "Описание фото")
```

## Тестирование

### Запустить тесты

```bash
go test -v ./...
```

### Создать простой тест

```go
// handlers/client_test.go

package handlers

import (
    "testing"
)

func TestHandleCatalog(t *testing.T) {
    // Тестовый код
}
```

## Логирование

Всегда логируйте важные события:

```go
// Успех
log.Printf("✅ Заказ #%d создан\n", orderID)

// Ошибка
log.Printf("❌ Ошибка создания заказа: %v\n", err)

// Событие
log.Printf("🔔 Новый пользователь %d\n", userID)

// Таймер
log.Printf("⏲️ Расписан таймер на %v\n", duration)
```

## Безопасность

### ✅ Правила для SQL запросов

```go
// ✅ ПРАВИЛЬНО - параметризованные запросы
_, err := db.Exec(ctx, 
    "SELECT * FROM users WHERE id = $1", 
    userID)

// ❌ НЕПРАВИЛЬНО - SQL injection
_, err := db.Exec(ctx, 
    fmt.Sprintf("SELECT * FROM users WHERE id = %d", userID))
```

### ✅ Проверка админ прав

```go
if !ah.IsAdmin(userID) {
    return c.Send("❌ Доступ запрещён")
}
```

### ✅ Валидация ввода

```go
if text == "" || len(text) < 3 {
    return c.Send("❌ Некорректный ввод")
}

// С регулярные выражениями
phoneRegex := regexp.MustCompile(`^\d{10,11}$`)
if !phoneRegex.MatchString(phone) {
    return c.Send("❌ Некорректный номер телефона")
}
```

## Debug режим

Установите переменную ENV для дополнительного логирования:

```bash
export ENV=development
go run main.go
```

## Common Issues

### Ошибка: "column "updated_at" not found"

Убедитесь что миграции выполнены. Проверьте логи:

```bash
docker-compose logs postgres
```

### Бот не отвечает на callbacks

Проверьте что callback data правильно форматирована в inline кнопках.

### Таймер не срабатывает

- Убедитесь что бот активен и получает обновления
- Проверьте логи scheduler.go
- Проверьте что контекст не был отменен

## Performance Tips

1. **Используйте индексы** - для часто запрашиваемых полей
2. **Кэшируйте** - часто используемые данные (настройки магазина)
3. **Пулируйте соединения** - используется pgxpool автоматически
4. **Избегайте N+1** - загружайте все данные в одном запросе
5. **Асинхронные операции** - используйте горутины для долгих операций

## Внесение изменений

1. Создайте новую branch: `git checkout -b feature/my-feature`
2. Выполните изменения
3. Запустите тесты: `go test ./...`
4. Форматируйте код: `go fmt ./...`
5. Commit: `git commit -m "feat: добавить мою фичу"`
6. Push: `git push origin feature/my-feature`
7. Создайте Pull Request

---

**Удачи в разработке! 🚀**
