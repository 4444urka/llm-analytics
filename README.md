# LLM Data Analytics

Веб-приложение для автоматического анализа данных с помощью ИИ-агента. Пользователь загружает CSV/Excel файл, LLM самостоятельно проводит анализ через вызов инструмента выполнения Python-кода — вычисляет метрики, строит графики, формирует инсайты.

## Как это работает

1. Загрузка датасета (CSV/Excel) через веб-интерфейс
2. Сервер читает структуру: колонки, типы, примеры строк → сохраняет в SQLite
3. Системный промпт + описание данных + инструкции пользователя → LLM
4. **LLM-агент вызывает `run_python` tool** — код исполняется в изолированной песочнице
5. Python (pandas, matplotlib, seaborn, sklearn) возвращает stdout, stderr, сгенерированные PNG-графики
6. LLM итерирует: анализирует результаты, вызывает инструменты повторно, пока не сформирует отчёт
7. Результат стримится через SSE — текст появляется постепенно, графики подгружаются

## Архитектура

```
llm-analytics/
├── backend/                  # Go REST API (net/http)
│   ├── main.go               # Точка входа, CORS, embed фронтенда
│   ├── config/config.go      # Конфиг + авто-загрузка .env
│   ├── db/db.go              # SQLite: сессии, датасеты, графики (BLOB)
│   ├── handler/handler.go    # HTTP: upload, analyze, SSE, results, charts
│   ├── agent/
│   │   ├── agent.go          # Агент: цикл LLM + tool calling + принудительный финиш
│   │   ├── llm.go            # OpenAI-совместимый клиент + SSE-стриминг
│   │   └── sandbox.go        # Python-песочница (subprocess, timeout, изоляция)
│   └── security/
│       ├── prompt.go         # Системный промпт
│       └── sanitize.go       # Защита от prompt-injection и code-injection
├── frontend/                 # React 18 + Vite + TypeScript
│   ├── src/
│   │   ├── App.tsx           # SSE-стриминг, Markdown, графики
│   │   ├── types.ts          # Типы API
│   │   └── components/       # UploadArea, Header, Card
│   └── vite.config.ts        # Прокси /api → backend, сборка в backend/frontend-dist/
├── Makefile                  # build, run, dev, clean
├── Dockerfile                # Multi-stage: node + go + python
└── README.md
```

## Технологии

| Слой | Технология |
|---|---|
| Фронтенд | React 18, TypeScript, Vite, react-markdown, remark-gfm |
| Бэкенд | Go (net/http), SQLite (modernc.org/sqlite), embed.FS |
| LLM API | DeepSeek / OpenAI-совместимые (function calling + SSE streaming) |
| Sandbox | Python 3 (pandas, numpy, matplotlib, seaborn, scikit-learn) |

## Быстрый старт

### Предварительные требования

- Go 1.21+
- Node.js 20+
- Python 3.10+ с пакетами: `pandas numpy matplotlib seaborn scikit-learn openpyxl`
- API ключ DeepSeek (https://platform.deepseek.com)

### 1. Установка Python-зависимостей

```bash
python3 -m venv .venv
source .venv/bin/activate
pip install pandas numpy matplotlib seaborn scikit-learn openpyxl
```

### 2. Настройка `.env`

Создать файл `.env` в корне проекта (переменные загружаются автоматически):

```bash
LLM_API_KEY=sk-your-deepseek-key
PYTHON_BIN=../.venv/bin/python3
```

### 3. Сборка и запуск (один бинарник)

```bash
make build   # frontend → backend/frontend-dist/ → go build
./backend/server
# Открыть http://localhost:8080
```

Или раздельно:

```bash
# Терминал 1 — бэкенд
cd backend && go run .

# Терминал 2 — фронтенд (dev с hot reload)
cd frontend && npm install && npm run dev
# → http://localhost:5173 (проксирует /api к :8080)
```

### Docker

```bash
docker build -t llm-analytics .
docker run -p 8080:8080 -e LLM_API_KEY=sk-xxx llm-analytics
```

## Переменные окружения

| Переменная | Назначение | По умолчанию |
|---|---|---|
| `LLM_API_KEY` | API ключ | **обязательно** |
| `LLM_API_BASE` | Базовый URL | `https://api.deepseek.com/v1` |
| `LLM_MODEL` | Модель | `deepseek-v4-flash` |
| `PORT` | Порт | `8080` |
| `DB_PATH` | Путь к SQLite | `./data.db` |
| `MAX_UPLOAD_SIZE_MB` | Макс. размер файла | `50` |
| `PYTHON_BIN` | Путь к Python 3 | `python3` |
| `PYTHON_TIMEOUT_SEC` | Таймаут кода | `60` |
| `FRONTEND_ORIGIN` | Origin для CORS (только dev) | _(пусто)_ |

## API

| Метод | Путь | Описание |
|---|---|---|
| `POST` | `/api/upload` | Загрузка CSV/Excel (multipart: `dataset`, `instructions`) |
| `POST` | `/api/analyze` | Блокирующий анализ (`{"session_id":"..."}`) |
| `POST` | `/api/analyze/stream` | SSE-стриминг (`text/event-stream`) |
| `GET` | `/api/results?session_id=...` | Результат: отчёт, графики, статус |
| `GET` | `/api/status?session_id=...` | Статус анализа |
| `GET` | `/charts/{name}?session_id=...` | PNG-график |
| `GET` | `/api/health` | Проверка: `{"status":"ok"}` |

## SSE-события стриминга

| `type` | `content` | Когда |
|---|---|---|
| `text` | Токен текста | LLM генерирует ответ |
| `status` | `"Running Python code..."` | Смена состояния агента |
| `done` | — | Агент завершил анализ |
| `charts` | `["chart1.png", ...]` | Графики сохранены |
| `complete` | Итоговый отчёт | Конец стрима |
| `error` | Текст ошибки | Ошибка |

## Особенности

### Агентный анализ
LLM не получает готовую статистику — он сам вызывает `run_python` tool до 20 итераций, каждая итерация: сгенерировать код → исполнить → получить результат → продолжить анализ. На 15-й итерации инструменты принудительно отключаются, LLM выдаёт финальный отчёт.

### Единый бинарник
`make build` собирает фронтенд в `backend/frontend-dist/`, Go встраивает его через `embed.FS`. Один файл `server` (~15 MB) содержит всё — бэкенд, API, SQLite, фронтенд.

### SQLite вместо файловой системы
Датасеты и графики хранятся как BLOB в таблицах `datasets` и `charts`. Сессии — в `sessions`. Никаких `uploads/`.

### Авто-загрузка `.env`
Сервер при старте ищет `.env` в текущей директории, родительской и рядом с бинарником. Существующие переменные окружения имеют приоритет.

## Защита от prompt injection

- Фильтрация пользовательских инструкций: regex-паттерны jailbreak, переопределения промпта, DAN
- Блокировка опасных Python-вызовов: `os.system`, `rm -rf`, `curl`, `wget`, `sudo`, `/bin/bash`, `eval()`, `exec()`, `compile()`
- Non-negotiable системный промпт
- Песочница: таймаут 60 сек, изолированный PATH, нет сети
- Валидация расширений: только `.csv`, `.xlsx`, `.xls`

## CI/CD

### CI (автоматически на push/PR в `main`)

`.github/workflows/ci.yml` — сборка фронтенда, прогон Go-тестов, сборка бинарника.

### CD / Деплой

`.github/workflows/deploy.yml` — сборка единого бинарника → SCP на сервер → systemd unit → перезапуск сервиса.

**Требования к серверу:**
- Linux с systemd
- Python 3.12+ с пакетами: `pandas numpy matplotlib seaborn scikit-learn openpyxl`

**Первичная настройка сервера (один раз):**

```bash
# Создать пользователя и директорию
sudo useradd -r -s /bin/false llm-analytics
sudo mkdir -p /opt/llm-analytics
sudo chown llm-analytics:llm-analytics /opt/llm-analytics

# Установить python-зависимости
pip install pandas numpy matplotlib seaborn scikit-learn openpyxl
```

**Secrets для GitHub Actions:**

| Secret | Назначение |
|---|---|
| `SSH_HOST` | IP/домен сервера |
| `SSH_PORT` | Порт SSH (обычно `22`) |
| `SSH_USER` | Пользователь с sudo (напр. `root`) |
| `SSH_PRIVATE_KEY` | Приватный SSH-ключ |
| `LLM_API_KEY` | API-ключ DeepSeek |
| `LLM_API_BASE` | `https://api.deepseek.com/v1` |
| `LLM_MODEL` | `deepseek-v4-flash` |
| `PORT` | `8080` |

После деплоя:

```bash
sudo systemctl status llm-analytics    # состояние
sudo journalctl -u llm-analytics -f    # логи
```

Ручной деплой (без GitHub Actions):

```bash
make build
scp backend/server user@host:/opt/llm-analytics/llm-analytics
scp deploy/llm-analytics.service user@host:/tmp/
ssh user@host "
  sudo mv /tmp/llm-analytics.service /etc/systemd/system/
  sudo systemctl daemon-reload
  sudo systemctl restart llm-analytics
"
```

## Тесты

```bash
cd backend
go test ./... -v -timeout 60s
```

Тесты покрывают: загрузку CSV, SQLite CRUD, интеграционные эндпоинты, безопасность, песочницу Python, CORS.

## Критерии (для курса)

- LLM как агент: вызывает `run_python` tool, не перефразирует готовые данные
- Инструкции пользователя: можно указать, на что обратить внимание
- Веб-интерфейс: React + Vite
- Защита от prompt injection: да
- SSE-стриминг: ответ появляется постепенно
- Единый бинарник: фронтенд встроен в Go
