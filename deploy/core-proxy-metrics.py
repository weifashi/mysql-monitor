#!/usr/bin/env python3
# coolify-proxy(Traefik v3)开 prometheus metrics :8082。幂等。
# 注意：Coolify 升级/重置 proxy 配置时可能覆盖此改动，届时监控会报采集失败。
import io
p = "/data/coolify/proxy/docker-compose.yml"
s = io.open(p).read()
if "metrics.prometheus" in s:
    print("已开启"); raise SystemExit
assert "      - '8080:8080'" in s and "      - '--ping=true'" in s
s = s.replace("      - '8080:8080'", "      - '8080:8080'\n      - '8082:8082'", 1)
s = s.replace("      - '--ping=true'",
    "      - '--ping=true'\n"
    "      - '--metrics.prometheus=true'\n"
    "      - '--metrics.prometheus.entryPoint=metrics'\n"
    "      - '--entryPoints.metrics.address=:8082'", 1)
io.open(p, "w").write(s)
print("已写入 metrics 配置")
