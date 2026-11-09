SHELL := /bin/sh

APP_DIR := $(CURDIR)/approaches/web-client-server
BACKEND_DIR := $(APP_DIR)/backend
FRONTEND_DIR := $(APP_DIR)/frontend
RUN_DIR := $(APP_DIR)/.run

BACKEND_ADDR ?= 127.0.0.1:8080
BACKEND_URL ?= http://$(BACKEND_ADDR)
FRONTEND_HOST ?= 127.0.0.1
FRONTEND_PORT ?= 5173
FRONTEND_URL ?= http://$(FRONTEND_HOST):$(FRONTEND_PORT)
DB_PATH ?= $(BACKEND_DIR)/letterpress.db
BOT_POLL_INTERVAL ?= 2s

SERVER_BIN := $(RUN_DIR)/letterpress-server
BOT_BIN := $(RUN_DIR)/letterpress-bot

.PHONY: help start stop status logs build

help:
	@echo "Letterpress development commands"
	@echo "  make start   Build and start the backend, frontend, and bot worker"
	@echo "  make stop    Stop only the processes previously started by make"
	@echo "  make status  Show managed process status and local URLs"
	@echo "  make logs    Show the latest managed process logs"
	@echo ""
	@echo "Override ports when needed, for example:"
	@echo "  make start BACKEND_ADDR=127.0.0.1:18080 FRONTEND_PORT=15174"

build:
	@mkdir -p "$(RUN_DIR)"
	@cd "$(BACKEND_DIR)" && go build -o "$(SERVER_BIN)" ./cmd/server
	@cd "$(BACKEND_DIR)" && go build -o "$(BOT_BIN)" ./cmd/bot-runtime

start: build
	@set -eu; \
	for service in backend frontend bot; do \
		pid_file="$(RUN_DIR)/$$service.pid"; \
		if [ -f "$$pid_file" ]; then \
			pid=$$(cat "$$pid_file"); \
			if [ -n "$$pid" ] && kill -0 "$$pid" 2>/dev/null; then \
				echo "$$service is already running with PID $$pid; run 'make stop' first" >&2; \
				exit 1; \
			fi; \
			rm -f "$$pid_file"; \
		fi; \
	done; \
	cleanup() { \
		for pid_file in "$(RUN_DIR)/frontend.pid" "$(RUN_DIR)/bot.pid" "$(RUN_DIR)/backend.pid"; do \
			if [ -f "$$pid_file" ]; then \
				pid=$$(cat "$$pid_file"); \
				if [ -n "$$pid" ] && kill -0 "$$pid" 2>/dev/null; then kill "$$pid" 2>/dev/null || true; fi; \
				rm -f "$$pid_file"; \
			fi; \
		done; \
	}; \
	trap cleanup EXIT INT TERM; \
	nohup "$(SERVER_BIN)" \
		-addr "$(BACKEND_ADDR)" \
		-db "$(DB_PATH)" \
		-insecure-cookies \
		-allowed-origin "$(FRONTEND_URL)" \
		>"$(RUN_DIR)/backend.log" 2>&1 & \
	echo $$! >"$(RUN_DIR)/backend.pid"; \
	ready=0; \
	for attempt in $$(seq 1 50); do \
		if curl -fsS "$(BACKEND_URL)/healthz" >/dev/null 2>&1; then ready=1; break; fi; \
		sleep 0.1; \
	done; \
	if [ "$$ready" -ne 1 ]; then \
		echo "backend did not become ready; see $(RUN_DIR)/backend.log" >&2; \
		tail -n 20 "$(RUN_DIR)/backend.log" >&2 || true; \
		exit 1; \
	fi; \
	bot_token=$$(openssl rand -base64 32 | tr '+/' '-_' | tr -d '='); \
	nohup env LETTERPRESS_BOT_TOKEN="$$bot_token" "$(BOT_BIN)" \
		-db "$(DB_PATH)" \
		-api-base-url "$(BACKEND_URL)" \
		-poll-interval "$(BOT_POLL_INTERVAL)" \
		>"$(RUN_DIR)/bot.log" 2>&1 & \
	echo $$! >"$(RUN_DIR)/bot.pid"; \
	bot_pid=$$(cat "$(RUN_DIR)/bot.pid"); \
	ready=0; \
	for attempt in $$(seq 1 50); do \
		if kill -0 "$$bot_pid" 2>/dev/null && grep -q '"msg":"bot runtime started"' "$(RUN_DIR)/bot.log"; then ready=1; break; fi; \
		if ! kill -0 "$$bot_pid" 2>/dev/null; then break; fi; \
		sleep 0.1; \
	done; \
	if [ "$$ready" -ne 1 ]; then \
		echo "bot worker did not become ready; see $(RUN_DIR)/bot.log" >&2; \
		tail -n 20 "$(RUN_DIR)/bot.log" >&2 || true; \
		exit 1; \
	fi; \
	nohup env LETTERPRESS_API_TARGET="$(BACKEND_URL)" \
		node "$(FRONTEND_DIR)/node_modules/vite/bin/vite.js" \
		"$(FRONTEND_DIR)" \
		--host "$(FRONTEND_HOST)" \
		--port "$(FRONTEND_PORT)" \
		--strictPort \
		>"$(RUN_DIR)/frontend.log" 2>&1 & \
	echo $$! >"$(RUN_DIR)/frontend.pid"; \
	frontend_pid=$$(cat "$(RUN_DIR)/frontend.pid"); \
	ready=0; \
	for attempt in $$(seq 1 50); do \
		if kill -0 "$$frontend_pid" 2>/dev/null && grep -q 'Local:' "$(RUN_DIR)/frontend.log"; then ready=1; break; fi; \
		if ! kill -0 "$$frontend_pid" 2>/dev/null; then break; fi; \
		sleep 0.1; \
	done; \
	if [ "$$ready" -ne 1 ]; then \
		echo "frontend did not become ready; see $(RUN_DIR)/frontend.log" >&2; \
		tail -n 20 "$(RUN_DIR)/frontend.log" >&2 || true; \
		exit 1; \
	fi; \
	trap - EXIT INT TERM; \
	echo "Letterpress started"; \
	echo "  Frontend: $(FRONTEND_URL)"; \
	echo "  Backend:  $(BACKEND_URL)"; \
	echo "  Logs:     $(RUN_DIR)"

stop:
	@set -eu; \
	stop_process() { \
		label="$$1"; pid_file="$$2"; expected="$$3"; \
		if [ ! -f "$$pid_file" ]; then echo "$$label is not running"; return; fi; \
		pid=$$(cat "$$pid_file"); \
		case "$$pid" in ''|*[!0-9]*) echo "invalid $$label PID file; removing it"; rm -f "$$pid_file"; return;; esac; \
		if ! kill -0 "$$pid" 2>/dev/null; then echo "$$label is not running"; rm -f "$$pid_file"; return; fi; \
		command=$$(ps -p "$$pid" -o command= 2>/dev/null || true); \
		case "$$command" in *"$$expected"*) ;; *) echo "refusing to stop PID $$pid: it is not the managed $$label process" >&2; return 1;; esac; \
		kill "$$pid"; \
		for attempt in $$(seq 1 50); do \
			if ! kill -0 "$$pid" 2>/dev/null; then break; fi; \
			sleep 0.1; \
		done; \
		if kill -0 "$$pid" 2>/dev/null; then kill -KILL "$$pid"; fi; \
		rm -f "$$pid_file"; \
		echo "stopped $$label"; \
	}; \
	stop_process frontend "$(RUN_DIR)/frontend.pid" "vite/bin/vite.js"; \
	stop_process bot "$(RUN_DIR)/bot.pid" "$(BOT_BIN)"; \
	stop_process backend "$(RUN_DIR)/backend.pid" "$(SERVER_BIN)"

status:
	@set -eu; \
	show_status() { \
		label="$$1"; pid_file="$$2"; \
		if [ -f "$$pid_file" ]; then \
			pid=$$(cat "$$pid_file"); \
			if [ -n "$$pid" ] && kill -0 "$$pid" 2>/dev/null; then echo "$$label: running (PID $$pid)"; return; fi; \
		fi; \
		echo "$$label: stopped"; \
	}; \
	show_status backend "$(RUN_DIR)/backend.pid"; \
	show_status frontend "$(RUN_DIR)/frontend.pid"; \
	show_status bot "$(RUN_DIR)/bot.pid"; \
	echo "Frontend: $(FRONTEND_URL)"; \
	echo "Backend:  $(BACKEND_URL)"

logs:
	@for service in backend frontend bot; do \
		echo "== $$service =="; \
		if [ -f "$(RUN_DIR)/$$service.log" ]; then tail -n 40 "$(RUN_DIR)/$$service.log"; else echo "No log yet."; fi; \
	done
