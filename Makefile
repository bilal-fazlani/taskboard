.PHONY: build dev dev-frontend frontend clean install test test-install

BUILD_DIR := cmd/taskboard
BINARY := taskboard
INSTALL_DIR ?= $(HOME)/.local/bin
PORT ?= 3010

# `make dev` must never touch the live board: it always uses a throwaway database
# inside the repo (.tmp/ is gitignored) and a port that does not collide with
# the installed server on $(PORT).
DEV_DB ?= ./.tmp/dev.db
DEV_PORT ?= 3011

build: frontend
	go build -o $(BINARY) ./$(BUILD_DIR)

dev:
	@test -n "$(DEV_DB)" || { echo "DEV_DB must not be empty (make dev must never use the live database)"; exit 1; }
	@test "$(DEV_PORT)" != "$(PORT)" || { echo "DEV_PORT must differ from the live server port $(PORT)"; exit 1; }
	mkdir -p $(dir $(DEV_DB))
	go run ./$(BUILD_DIR) --db $(DEV_DB) start --foreground --port $(DEV_PORT)

frontend:
	cd web && npm install && npm run build
	mkdir -p $(BUILD_DIR)/web/dist
	cp -r web/dist/* $(BUILD_DIR)/web/dist/

clean:
	rm -f $(BINARY)
	rm -rf $(BUILD_DIR)/web
	rm -rf web/dist web/node_modules

# Stops every process using the live database, backs it up, installs the new
# binary and starts the server. See scripts/install.sh.
install: build
	INSTALL_DIR="$(INSTALL_DIR)" PORT="$(PORT)" ./scripts/install.sh $(BINARY)

# Proxies /api to the `make dev` backend on $(DEV_PORT), not the live server.
dev-frontend:
	cd web && TASKBOARD_API_PORT=$(DEV_PORT) npm run dev

test:
	go test ./...

# Runs scripts/install.sh end to end in a sandboxed HOME; never touches the live board.
test-install: build
	./scripts/install_test.sh $(BINARY)
