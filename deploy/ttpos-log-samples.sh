#!/bin/sh
# 错误样本服务的处理端。由 systemd socket activation 拉起：
# 平时没有进程、零内存；有连接进来才跑这一下，输出 HTTP 响应后退出。
# 内容是日志扫描留存的最近错误行（已剥 docker json 外壳、已排噪音）。
set -u
# 读掉请求头（直到空行），避免对端收到 RST
while IFS= read -r line; do
  line=$(printf %s "$line" | tr -d '\r')
  [ -z "$line" ] && break
done
F=/var/lib/ttpos-log-metrics/recent.log
if [ -s "$F" ]; then
  BODY=$(tail -c 12000 "$F")
else
  BODY="(近期无错误样本)"
fi
LEN=$(printf '%s' "$BODY" | wc -c)
printf 'HTTP/1.0 200 OK\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: %s\r\nConnection: close\r\n\r\n' "$LEN"
printf '%s' "$BODY"
