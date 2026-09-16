.PHONY: build dev dev-frontend frontend clean install test test-install test-guard test-web

BUILD_DIR := cmd/taskboard
BINARY := taskboard
INSTALL_DIR ?= $(HOME)/.local/bin
PORT ?= 3010

# `make dev` must never touch the live board: it always uses a throwaway database
# inside the repo (.tmp/ is gitignored) and a port that does not collide with
# the installed server on $(PORT).
DEV_DB ?= ./.tmp/dev.db
DEV_PORT ?= 3011

# The binary shipped by `make install` and release binaries (see release.yml) are
# the marked builds: only they may open the default database, and they default
# to port 3010. Every other build (make build, make dev, go build, go run,
# go test) needs --db and defaults to 3011. The `make install` binary is built
# under .tmp/live, not as ./taskboard, and is deleted once used, so no marked
# build is left lying around to be run by accident.
LIVE_LDFLAGS := -X github.com/tcarac/taskboard/internal/livebuild.Mark=true
LIVE_BINARY := .tmp/live/taskboard

define build-live
	@mkdir -p $(dir $(LIVE_BINARY))
	go build -ldflags '$(LIVE_LDFLAGS)' -o $(LIVE_BINARY) ./$(BUILD_DIR)
endef

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
	rm -rf $(dir $(LIVE_BINARY))

# Stops every process using the live database, backs it up, installs the new
# binary and starts the server. See scripts/install.sh.
install: frontend
	$(build-live)
	INSTALL_DIR="$(INSTALL_DIR)" PORT="$(PORT)" ./scripts/install.sh $(LIVE_BINARY); status=$$?; rm -f $(LIVE_BINARY); exit $$status

# Proxies /api to the `make dev` backend on $(DEV_PORT), not the live server.
dev-frontend:
	cd web && TASKBOARD_API_PORT=$(DEV_PORT) npm run dev

test: test-guard test-web
	go test ./...

# Runs scripts/install.sh end to end in a sandboxed HOME with the same marked
# build `make install` ships; never touches the live board.
test-install: frontend
	$(build-live)
	./scripts/install_test.sh $(LIVE_BINARY); status=$$?; rm -f $(LIVE_BINARY); exit $$status

# Feeds sample commands to the Claude Code PreToolUse hook and checks allow/deny.
test-guard:
	bash scripts/claude-guard_test.sh

# Runs the web unit tests once with vitest. Needs no frontend build or server.
test-web:
	cd web && npm install && npm test
