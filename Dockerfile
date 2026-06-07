FROM node:20-alpine AS frontend-builder

WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci --silent 2>/dev/null || npm install --silent
COPY frontend/ .
RUN npm run build

FROM golang:1.23-alpine AS backend-builder

WORKDIR /app
COPY backend/go.mod backend/go.sum* ./
RUN go mod download 2>/dev/null; exit 0
COPY backend/ .
COPY --from=frontend-builder /app/frontend/frontend-dist ./frontend-dist
RUN CGO_ENABLED=0 go build -o server .

FROM python:3.12-alpine

RUN pip install --no-cache-dir \
    pandas numpy matplotlib seaborn scikit-learn openpyxl

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=backend-builder /app/server .

EXPOSE 8080

ENV PYTHON_BIN=python3
ENV PYTHON_TIMEOUT_SEC=60
ENV DB_PATH=./data.db
ENV PORT=8080
ENV FRONTEND_ORIGIN=*

CMD ["./server"]
