FROM golang:1.25-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o monitor .

# 健康检查的触发动作用 `go tool pprof` 做自动诊断（界面上的 pprof 模板也生成这些命令）。
# 单独编译 google/pprof 约 9MB，比在运行镜像里装整个 Go 工具链（约 176MB）划算。
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go install github.com/google/pprof@latest

FROM alpine:3.19
RUN apk --no-cache add ca-certificates tzdata curl
WORKDIR /app
COPY --from=builder /build/monitor .
COPY --from=builder /go/bin/pprof /usr/local/bin/pprof
# `go tool pprof` 的最小转发层：只用到这一个子命令，没必要带整个 Go。
RUN printf '%s\n' '#!/bin/sh' \
    'if [ "$1" = "tool" ] && [ "$2" = "pprof" ]; then shift 2; exec /usr/local/bin/pprof "$@"; fi' \
    'echo "此镜像只提供 go tool pprof，未安装完整 Go 工具链。收到：go $*" >&2; exit 127' \
    > /usr/local/bin/go && chmod +x /usr/local/bin/go
ENV TZ=Asia/Shanghai
EXPOSE 8080
VOLUME ["/app/data"]
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD wget -qO- http://localhost:8080/login || exit 1
ENTRYPOINT ["./monitor"]
