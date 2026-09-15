.PHONY: build dev frontend clean install test test-install

BUILD_DIR := cmd/taskboard
BINARY := taskboard
INSTALL_DIR ?= $(HOME)/.local/bin
PORT ?= 3010

build: frontend
	go build -o $(BINARY) ./$(BUILD_DIR)

dev:
	go run ./$(BUILD_DIR) start --foreground

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

dev-frontend:
	cd web && npm run dev

test:
	go test ./...

# Runs scripts/install.sh end to end in a sandboxed HOME; never touches the live board.
test-install: build
	./scripts/install_test.sh $(BINARY)
