# LLM Data Analytics

Web-приложение для автоматического анализа данных с помощью ИИ-агента. Пользователь загружает CSV/Excel файл, а LLM самостоятельно проводит анализ через вызов инструмента выполнения Python-кода — вычисляет метрики, строит графики и формирует инсайты.

## Как это работает

1. Пользователь загружает датасет через веб-интерфейс
2. Сервер читает структуру файла (колонки, строки, примеры данных)
3. Системный промпт с описанием данных и инструкциями пользователя → LLM
4. **LLM как агент вызывает `run_python` tool** — выполняет код в песочнице
5. Python-код исполняется (pandas, matplotlib, seaborn, sklearn), stdout/stderr/графики возвращаются LLM
6. LLM итерирует: анализирует результаты, вызывает инструменты снова
7. Финальный отчёт с метриками, графиками и инсайтами → пользователю

## Архитектура

```
llm-analytics/
├── backend/               # Go REST API (net/http)
│   ├── main.go            # Точка входа, CORS, статика
│   ├── config/config.go   # Конфигурация (env)
│   ├── handler/handler.go # HTTP-обработчики API
│   ├── agent/
│   │   ├── agent.go       # Оркестрация LLM + tool calling
│   │   ├── llm.go         # Клиент LLM API (OpenAI-совместимый)
│   │   └── sandbox.go     # Python-песочница
│   ├── session/store.go   # Хранение сессий
│   └── security/
│       ├── prompt.go      # Системный промпт
│       └── sanitize.go    # Защита от инъекций
├── frontend/              # React + Vite + TypeScript
│   ├── src/
│   │   ├── App.tsx        # Главный компонент
│   │   ├── types.ts       # Типы API
│   │   └── styles.css     # Стили (GitHub dark theme)
│   ├── vite.config.ts     # Конфиг Vite (прокси к бэкенду)
│   └── package.json
├── Dockerfile             # Multi-stage сборка
└── README.md
```

## Технологии

| Слой | Технология |
|---|---|
| Фронтенд | React 18, TypeScript, Vite, react-markdown |
| Бэкенд | Go (стандартная библиотека `net/http`) |
| LLM API | GitHub Models (Azure AI Inference) — **бесплатно** |
| Sandbox | Python (pandas, numpy, matplotlib, seaborn, scikit-learn) |

## LLM API: GitHub Models

Используется **GitHub Models** — бесплатный доступ к GPT-4o, Llama, Mistral и др. через Azure AI Inference API.

1. Получить токен: https://github.com/settings/tokens (никакие scopes не нужны)
2. API совместим с OpenAI форматом, меняется только base URL
3. Бесплатные лимиты: достаточны для тестирования и небольших задач

## Быстрый старт

### 1. Бэкенд

```bash
cd backend

# Установка Python-зависимостей
python3 -m venv .venv
source .venv/bin/activate
pip install pandas numpy matplotlib seaborn scikit-learn openpyxl

# Настройка API ключа GitHub Models
export LLM_API_KEY=ghp_your_github_token
export PYTHON_BIN=.venv/bin/python3

# Запуск
go run .
# API: http://localhost:8080
```

### 2. Фронтенд

```bash
cd frontend
npm install
npm run dev
# Открыть http://localhost:5173
```

Vite проксирует `/api/*` и `/charts/*` на бэкенд (порт 8080).

### 3. Production (монолит)

```bash
cd frontend && npm run build   # сборка в frontend/dist/
cd ../backend
export LLM_API_KEY=ghp_your_token
export PYTHON_BIN=.venv/bin/python3
go run .
# Открыть http://localhost:8080 (сервер раздаёт и API, и статику)
```

### Docker

```bash
docker build -t llm-analytics .
docker run -p 8080:8080 \
  -e LLM_API_KEY=ghp_your_token \
  llm-analytics
```

## Переменные окружения

| Переменная | Назначение | По умолчанию |
|---|---|---|
| `LLM_API_KEY` | GitHub токен или OpenAI ключ | **обязательно** |
| `LLM_API_BASE` | Базовый URL LLM API | `https://models.inference.ai.azure.com` |
| `LLM_MODEL` | Модель | `gpt-4o` |
| `PORT` | Порт бэкенда | `8080` |
| `FRONTEND_ORIGIN` | Origin фронтенда для CORS | `http://localhost:5173` |
| `UPLOAD_DIR` | Директория загрузок | `./uploads` |
| `MAX_UPLOAD_SIZE_MB` | Макс. размер файла | `50` |
| `PYTHON_BIN` | Путь к Python 3 | `python3` |
| `PYTHON_TIMEOUT_SEC` | Таймаут выполнения кода | `60` |

## Доступные модели GitHub Models

| Модель | `LLM_MODEL` |
|---|---|
| GPT-4o | `gpt-4o` |
| GPT-4o-mini | `gpt-4o-mini` |
| Llama 3.3 70B | `Llama-3.3-70B-Instruct` |
| Mistral Large | `Mistral-large` |
| Phi-4 | `Phi-4` |
| DeepSeek V3 | `DeepSeek-V3` |

## API

### `POST /api/upload`
Загрузка датасета. `multipart/form-data`: `dataset` (файл), `instructions` (опционально).

### `POST /api/analyze`
Запуск анализа. `{"session_id": "..."}`.

### `GET /api/results?session_id=...`
Получение результатов.

### `GET /charts/{name}?session_id=...`
Сгенерированный график (PNG).

### `GET /api/status?session_id=...`
Статус анализа.

### `GET /api/health`
Проверка работоспособности.

## Защита от prompt injection

- Фильтрация инструкций пользователя: regex-паттерны jailbreak, переопределения системного промпта
- Блокировка опасных Python-вызовов (`os.system`, `subprocess`, `eval`/`exec`, сетевые операции)
- Non-negotiable системный промпт
- Таймаут выполнения кода (60 сек)
- Валидация расширений файлов

## Критерии (для курса)

- LLM как агент: вызывает tool `run_python`, а не перефразирует готовые данные
- Инструкции пользователя: можно указать, на что обратить внимание
- Веб-интерфейс: React + Vite
- Защита от prompt injection: да
