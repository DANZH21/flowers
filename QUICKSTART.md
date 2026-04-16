# 📋 ИНСТРУКЦИЯ ПО ЗАПУСКУ FLOWER BOT

## ✅ Что было создано

Полнофункциональный Telegram-бот для цветочного магазина на Go. Проект включает:

### 📁 Структура файлов

```
flower-bot/
├── main.go                  # Основное приложение с обработчиками
├── go.mod                   # Go модули
├── go.sum                   # Хеши зависимостей
├── Dockerfile              # Multi-stage Docker build
├── docker-compose.yml      # Docker Compose конфиграция
├── Makefile                # Команды для разработки
├── .env.example            # Пример конфигурации
├── .gitignore              # Git правила
├── README.md               # Основная документация
├── DEVELOPMENT.md          # Руководство для разработчиков
│
├── config/
│   └── config.go           # Загрузка конфигурации из .env
│
├── db/
│   ├── db.go               # Подключение к PostgreSQL (pgxpool)
│   └── migrations.go       # Автоматические миграции при старте
│
├── models/
│   └── models.go           # Все структуры данных (User, Order, Bouquet и т.д.)
│
├── handlers/
│   ├── states.go           # StateManager для FSM состояний
│   ├── client.go           # Все обработчики для клиентов (~700 строк)
│   └── admin.go            # Все обработчики для админов (~800 строк)
│
└── scheduler/
    └── scheduler.go        # Планировщик таймеров (резерв, чеки, и т.д.)
```

### 🔧 Стек технологий

✅ **Go 1.21+**
✅ **telebot.v3** - Telegram Bot API
✅ **PostgreSQL 16** + **pgx/v5** - база данных
✅ **godotenv** - конфигурация
✅ **Docker & Docker Compose**
✅ **time.AfterFunc** - планировщик

### ⚡ Функциональность

#### 🛍️ Для клиентов
- ✅ Просмотр каталога букетов с фото
- ✅ Заказ букетов с доставкой/самовывозом
- ✅ Оплата через Kaspi Pay или наличные
- ✅ Загрузка чеков и подтверждение оплаты
- ✅ Заказ кастомных букетов
- ✅ Просмотр истории заказов
- ✅ Контакты поддержки и информацию о магазине

#### 👨‍💼 Для администраторов
- ✅ Управление заказами (подтверждение, отмена, отслеживание)
- ✅ Управление каталогом букетов (добавление, редактирование, удаление)
- ✅ Управление кастомными заказами
- ✅ Управление настройками магазина
- ✅ Просмотр статистики

#### ⏲️ Автоматизация
- ✅ Резервирование букетов на 30 минут
- ✅ Автоматическая отмена заказа если чек не загружен за 30 минут
- ✅ Уведомления админу при новых заказах
- ✅ Защита от спама (один заказ в 30 минут)

---

## 🚀 БЫСТРЫЙ СТАРТ

### Вариант 1: Docker Compose (Рекомендуется)

**Требования:**
- Docker
- Docker Compose

**Шаги:**

```bash
# 1. Перейдите в папку проекта
cd c:\Users\dange\OneDrive\Рабочий\ стол\flowers

# 2. Создайте файл .env
cp .env.example .env

# 3. Отредактируйте .env в редакторе (ВАЖНО!)
# Заполните следующие значения:
# - TELEGRAM_BOT_TOKEN (получить от BotFather в Telegram)
# - ADMIN_IDS (ваш Telegram ID, можно получить у @userinfobot)
# - Остальные параметры магазина

# 4. Запустите Docker Compose
docker-compose up -d

# 5. Проверьте логи
docker-compose logs -f bot

# 6. Для остановки
docker-compose down
```

### Вариант 2: Локальный запуск

**Требования:**
- Go 1.21+
- PostgreSQL 16+

**Шаги:**

```bash
# 1. Установите зависимости
go mod download
go mod tidy

# 2. Создайте .env файл
cp .env.example .env

# 3. Отредактируйте .env и задайте параметры
# Особенно DATABASE_URL для вашей локальной БД

# 4. Запустите бота
go run main.go

# 5. В отдельном терминале для Docker Postgres:
docker run --name flower-db \
  -e POSTGRES_USER=flower_user \
  -e POSTGRES_PASSWORD=flower_password \
  -e POSTGRES_DB=flower_bot \
  -p 5432:5432 \
  postgres:16-alpine
```

---

## 📝 ПЕРЕМЕННЫЕ ОВЕ ОКРУЖЕНИЯ (.env)

```env
# ❗ ОБЯЗАТЕЛЬНЫЕ
TELEGRAM_BOT_TOKEN=123456789:ABCDefGHIjklmnoPQRstuvWXYZabcdefgh
# Получить от @BotFather в Telegram
# Инструкция: https://core.telegram.org/bots#botfather

ADMIN_IDS=123456789,987654321
# Ваш Telegram ID (можно получить у @userinfobot)
# Несколько ID разделяются запятыми

DATABASE_URL=postgres://flower_user:flower_password@postgres:5432/flower_bot
# Для Docker: используйте postgres:5432
# Для локального PostgreSQL: используйте localhost:5432

# 📦 Параметры БД
DB_HOST=postgres
DB_PORT=5432
DB_USER=flower_user
DB_PASSWORD=flower_password
DB_NAME=flower_bot

# 🏪 Параметры магазина (можно редактировать в админ-панели)
SHOP_NAME=Flower Shop
SHOP_ADDRESS=Almaty, Kazakhstan
SUPPORT_USER_ID=123456789
KASPI_LINK=https://kaspi.kz/shop/...
ABOUT_CHANNEL_LINK=https://t.me/flower_channel

# 🔧 Окружение
ENV=production  # или development для debug режима
```

---

## 💻 ИСПОЛЬЗОВАНИЕ БОТА

### Для клиентов

1. **Найдите бота в Telegram** по нику (который вы указали @BotFather)
2. **Нажмите /start** для начала
3. **Используйте кнопки меню:**
   - 🌸 **Каталог** - просмотр всех букетов
   - 📦 **Мои заказы** - история ваших заказов
   - 🎨 **Свой букет** - заказать кастомный букет
   - ℹ️ **О нас** - ссылка на канал
   - 📍 **Адрес** - адрес магазина
   - 💬 **Поддержка** - связь с поддержкой

### Для администраторов

1. **Отправьте /admin** боту
2. **Выберите опцию:**
   - 📦 **Заказы** - управление заказами
   - 🌸 **Каталог** - добавление/редактирование букетов
   - ⚙️ **Настройки** - настройки магазина
   - 📊 **Статистика** - просмотр статистики

---

## 🔒 БЕЗОПАСНОСТЬ

✅ **SQL Injection Protection** - все запросы параметризованы
✅ **Admin Access Control** - проверка прав перед каждым действием
✅ **Double Confirmation** - подтверждение критических операций
✅ **Graceful Shutdown** - корректное завершение
✅ **Rate Limiting** - защита от спама

---

## 🛠️ КОМАНДЫ MAKEFILE

```bash
make help              # Показать все команды
make build             # Compilar приложение
make run               # Запустить локально
make docker-up         # Запустить Docker Compose
make docker-down       # Остановить Docker Compose
make docker-logs       # Показать логи
make fmt               # Форматировать код
make deps              # Загрузить зависимости
make clean             # Очистить артефакты
```

---

## 📊 БАЗА ДАННЫХ

Все таблицы создаются автоматически при старте:

- **shop_settings** - настройки магазина
- **users** - пользователи (telegram_id, имя, телефон, админ статус)
- **bouquets** - каталог букетов (с информацией о резерве)
- **orders** - заказы (со статусами: pending, confirmed, delivering, completed, cancelled)
- **custom_orders** - кастомные заказы

Все запросы используют параметризованные версии через pgx для защиты от SQL injection.

---

## 📱 ПРИМЕРЫ ЭКРАНОВ / FLOW

### Заказ букета:
```
👤 "Как вас зовут?" 
   → Пользователь вводит имя

📱 "Ваш номер телефона?"
   → Пользователь вводит номер

📦 "Выберите тип доставки:"
   [🚚 Доставка] [🏪 Самовывоз]

💳 "Выберите способ оплаты:"
   [💳 Kaspi Pay] [💵 Наличные]

📋 "Ваш заказ:" (итоговое подтверждение)
   [✅ Подтвердить] [❌ Отмена]

💳 "Оплатите и отправьте чек:"
   (ссылка на Kaspi Pay)
   ⏰ 30 минут на отправку

✅ "Ожидайте подтверждения администратора"
```

### Админ-панель:
```
⚙️ АДМИН ПАНЕЛЬ
[📦 Заказы] [🌸 Каталог]
[⚙️ Настройки] [📊 Статистика]

📦 АКТИВНЫЕ ЗАКАЗЫ
[🟡 #1 — Иван — Роза]
[🟢 #2 — Мария — Тюльпаны]

→ Нажимаем на заказ → Видим детали и кнопки действия
```

---

## 🐛 TROUBLESHOOTING

### Проблема: "Бот не отвечает"
**Решение:**
1. Проверьте что TELEGRAM_BOT_TOKEN правильный
2. Убедитесь что токен не истек
3. Проверьте логи: `docker-compose logs bot`

### Проблема: "Ошибка подключения к БД"
**Решение:**
1. Проверьте DATABASE_URL формат
2. Убедитесь что PostgreSQL запущен
3. Проверьте учетные данные в .env

### Проблема: "Миграции не выполнились"
**Решение:**
1. Проверьте логи PostgreSQL: `docker-compose logs postgres`
2. Убедитесь что БД существует
3. Перезапустите контейнеры: `docker-compose down && docker-compose up -d`

### Проблема: "Admin кнопки не работают"
**Решение:**
1. Проверьте что ваш Telegram ID в ADMIN_IDS
2. Убедитесь что вы используете /admin команду
3. Проверьте что в БД вы админ (is_admin=true)

---

## 📚 ДОПОЛНИТЕЛЬНАЯ ДОКУМЕНТАЦИЯ

- **README.md** - основная документация проекта
- **DEVELOPMENT.md** - руководство для разработчиков с примерами кода
- **main.go** - основное приложение с комментариями
- **handlers/*.go** - все обработчики с подробными комментариями

---

## 🌐 РАЗВЕРТЫВАНИЕ НА СЕРВЕР

### Используя Docker

```bash
# 1. Коммитим код в Git
git add .
git commit -m "initial commit"
git push origin main

# 2. На сервере клонируем
git clone <repo-url>
cd flower-bot

# 3. Создаем .env с правильными значениями
cp .env.example .env
# Отредактируйте .env

# 4. Запускаем Docker Compose
docker-compose up -d

# 5. Проверяем логи
docker-compose logs -f bot

# 6. Для обновления
git pull origin main
docker-compose down
docker-compose up -d
```

### Используя systemd (без Docker)

Создайте файл `/etc/systemd/system/flower-bot.service`:

```ini
[Unit]
Description=Flower Bot
After=network.target postgresql.service

[Service]
Type=simple
User=flower
WorkingDirectory=/opt/flower-bot
Environment="PATH=/usr/local/go/bin:/opt/flower-bot:$PATH"
EnvironmentFile=/opt/flower-bot/.env
ExecStart=/opt/flower-bot/flower-bot
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

Запустите:
```bash
sudo systemctl enable flower-bot
sudo systemctl start flower-bot
sudo systemctl status flower-bot
```

---

## 💬 ПОДДЕРЖКА

Если у вас есть вопросы или проблемы:

1. Проверьте DEVELOPMENT.md
2. Посмотрите логи бота
3. Убедитесь что все переменные окружения установлены
4. Проверьте что PostgreSQL запущен и доступен

---

## 📄 ЛИЦЕНЗИЯ

MIT - вы можете использовать этот код в своих проектах

---

## ✨ УСПЕШНОГО ЗАПУСКА!

Если все прошло правильно, ваш бот должен:
✅ Подключиться к Telegram
✅ Подключиться к PostgreSQL
✅ Создать все таблицы
✅ Быть готовым к первому заказу

**Поздравляем! Ваш цветочный магазин теперь в Telegram! 🌸**

---

Последнее обновление: Март 2026
Версия: 1.0.0
