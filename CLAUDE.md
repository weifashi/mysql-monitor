# MySQL Monitor

## 构建与运行

所有构建和重启操作统一使用：

```bash
make up
```

此命令会自动完成 Docker 构建并启动服务。

## 其他常用命令

- `make down` — 停止服务
- `make logs` — 查看日志
- `make status` — 查看状态
- `make clean` — 清理容器和镜像

## 项目结构

- `internal/` — Go 业务代码
- `internal/web/static/` — 前端静态文件 (Vue3 + NaiveUI)
- `main.go` — 入口
- `.env` — 环境变量配置（不提交）
