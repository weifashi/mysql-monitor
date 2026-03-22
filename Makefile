.PHONY: help build run up down restart logs status clean init

DOCKER_COMPOSE ?= docker compose
ENV_FILE := .env
COMPOSE_FILE := docker-compose.yml
COMPOSE_EXAMPLE := docker-compose.example.yml

help: ## 显示帮助信息
	@echo "════════════════════════════════════════════════════════"
	@echo "  MySQL Monitor - Web 管理界面"
	@echo "════════════════════════════════════════════════════════"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'
	@echo ""

init: ## 从示例生成 docker-compose.yml（已存在则跳过）
	@if [ ! -f $(COMPOSE_FILE) ]; then \
		cp $(COMPOSE_EXAMPLE) $(COMPOSE_FILE); \
		echo "✅ 已创建 $(COMPOSE_FILE)"; \
	else \
		echo "⚠️  $(COMPOSE_FILE) 已存在，跳过"; \
	fi

build: ## 本地编译
	@echo "🔨 编译中..."
	go build -o mysql-monitor .
	@echo "✅ 编译完成: ./mysql-monitor"

run: build ## 本地运行
	@if [ ! -f $(ENV_FILE) ]; then cp .env.example $(ENV_FILE); echo "已创建 .env，请修改后重新运行"; exit 1; fi
	@export $$(grep -v '^#' $(ENV_FILE) | xargs) && ./mysql-monitor

up: ## Docker 启动
	@if [ ! -f $(COMPOSE_FILE) ]; then cp $(COMPOSE_EXAMPLE) $(COMPOSE_FILE); echo "✅ 已创建 $(COMPOSE_FILE)"; fi
	@if [ ! -f $(ENV_FILE) ]; then cp .env.example $(ENV_FILE); echo "已创建 .env，请修改 ADMIN_PASSWORD 后重新运行"; exit 1; fi
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) --env-file $(ENV_FILE) up -d --build
	@echo "✅ 已启动: http://localhost:$${WEB_PORT:-8080}"

down: ## Docker 停止
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) down

restart: ## Docker 重启
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) restart

logs: ## 查看日志
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) logs -f

status: ## 查看状态
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) ps

clean: ## 清理（容器+镜像）
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) down --rmi all -v
