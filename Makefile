.PHONY: build run dev clean

build:
	cd frontend && npm install --silent && npm run build
	cd backend && go build -o server .

run: build
	cd backend && ./server

dev:
	cd backend && go run . &
	cd frontend && npm run dev

clean:
	rm -f backend/server backend/data.db
	rm -rf backend/frontend-dist frontend/dist
