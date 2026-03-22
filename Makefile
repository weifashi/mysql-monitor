.PHONY: help build up down restart logs status clean init

# Docker Compose 文件
COMPOSE_FILE := docker-compose.yml
COMPOSE_FILE_EXAMPLE := docker-compose.example.yml
ENV_FILE := .env
# V2 为 `docker compose`；若只有旧版可执行文件: make DOCKER_COMPOSE=docker-compose up
DOCKER_COMPOSE ?= docker compose

help: ## 显示帮助信息
	@echo "════════════════════════════════════════════════════════════════"
	@echo "  MySQL Performance Schema 监控工具 - Docker Compose 管理"
	@echo "════════════════════════════════════════════════════════════════"
	@echo ""
	@echo "使用方法: make <command>"
	@echo ""
	@echo "命令列表:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'
	@echo ""
	@echo "════════════════════════════════════════════════════════════════"

init: ## 初始化配置文件
	@echo "📝 初始化配置文件..."
	@if [ ! -f $(COMPOSE_FILE) ]; then \
		cp $(COMPOSE_FILE_EXAMPLE) $(COMPOSE_FILE); \
		echo "✅ 已创建 $(COMPOSE_FILE)，可根据需要修改"; \
	else \
		echo "⚠️  $(COMPOSE_FILE) 已存在，跳过"; \
	fi
	@if [ ! -f $(ENV_FILE) ]; then \
		cp .env.example $(ENV_FILE); \
		echo "✅ 已创建 $(ENV_FILE)，请编辑配置后运行 make up"; \
	else \
		echo "⚠️  $(ENV_FILE) 已存在，跳过"; \
	fi

build: ## 构建 Docker 镜像
	@echo "🔨 构建监控工具镜像..."
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) --env-file $(ENV_FILE) build
	@echo "✅ 镜像构建完成"

up: ## 启动所有监控服务
	@if [ ! -f $(ENV_FILE) ]; then \
		echo "❌ 配置文件 $(ENV_FILE) 不存在，请先运行: make init"; \
		exit 1; \
	fi
	@echo "🚀 启动监控服务..."
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) --env-file $(ENV_FILE) up -d
	@echo "✅ 监控服务已启动"
	@echo ""
	@make status

up-dev: ## 仅启动开发环境监控
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) --env-file $(ENV_FILE) up -d monitor-dev
	@echo "✅ 开发环境监控已启动"

up-test: ## 仅启动测试环境监控
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) --env-file $(ENV_FILE) up -d monitor-test
	@echo "✅ 测试环境监控已启动"

up-prod: ## 仅启动生产环境监控
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) --env-file $(ENV_FILE) up -d monitor-prod
	@echo "✅ 生产环境监控已启动"

down: ## 停止并删除所有监控服务
	@echo "🛑 停止监控服务..."
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) down
	@echo "✅ 监控服务已停止"

stop: ## 停止所有监控服务（不删除）
	@echo "⏸️  暂停监控服务..."
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) stop
	@echo "✅ 监控服务已暂停"

start: ## 启动已存在的监控服务
	@echo "▶️  启动监控服务..."
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) start
	@echo "✅ 监控服务已启动"

restart: ## 重启所有监控服务
	@echo "🔄 重启监控服务..."
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) restart
	@echo "✅ 监控服务已重启"

restart-dev: ## 重启开发环境监控
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) restart monitor-dev

restart-test: ## 重启测试环境监控
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) restart monitor-test

restart-prod: ## 重启生产环境监控
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) restart monitor-prod

status: ## 查看监控服务状态
	@echo "📊 监控服务状态:"
	@$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) ps

logs: ## 查看所有监控服务日志（实时）
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) logs -f

logs-dev: ## 查看开发环境监控日志
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) logs -f monitor-dev

logs-test: ## 查看测试环境监控日志
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) logs -f monitor-test

logs-prod: ## 查看生产环境监控日志
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) logs -f monitor-prod

logs-tail: ## 查看最近 100 行日志
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) logs --tail=100

update: ## 更新代码并重建
	@echo "🔄 更新监控工具..."
	@make build
	@echo "🔄 重新创建容器..."
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) --env-file $(ENV_FILE) up -d --force-recreate
	@echo "✅ 更新完成"

update-config: ## 应用配置更新（不重新构建）
	@echo "🔄 应用配置更新..."
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) --env-file $(ENV_FILE) up -d --force-recreate
	@echo "✅ 配置已更新"

clean: ## 清理所有监控资源（容器、镜像、网络）
	@echo "🗑️  清理监控资源..."
	$(DOCKER_COMPOSE) -f $(COMPOSE_FILE) down --rmi all -v
	@echo "✅ 清理完成"

test: ## 测试监控工具（本地编译方式）
	@echo "🧪 测试监控工具..."
	@cd main && go build -o ../perf_monitor . && cd ..
	@echo "✅ 编译成功"
	@./perf_monitor -host=35.240.129.50 -port=58888 -user=dev -password=s7cx5CcfBanwq0LbZ9 -interval=5 -threshold=5

shell-dev: ## 进入开发环境监控容器
	docker exec -it mysql-monitor-dev /bin/sh

shell-test: ## 进入测试环境监控容器
	docker exec -it mysql-monitor-test /bin/sh

shell-prod: ## 进入生产环境监控容器
	docker exec -it mysql-monitor-prod /bin/sh
