#!/usr/bin/env python3
# 给三个网关的 nginx.conf 追加 stub_status server（http 块末尾）。
# 配置是 bind-mount 单文件：必须原地覆写（open 'w'），绝不 mv/sed -i。
import io, os, shutil, time
CONFS = [
    ("/etc/ttpos-edge/regional-origin/nginx.conf", 8083),
    ("/etc/ttpos-edge/core-shadow-gateway.nginx.conf", 8084),
    ("/etc/ttpos-edge/release-object-gateway.nginx.conf", 8085),
]
BK = "/root/nginx-conf-backup-" + time.strftime("%Y%m%d-%H%M%S")
os.makedirs(BK, exist_ok=True)
for path, port in CONFS:
    if not os.path.exists(path):
        print(f"SKIP {path} 不存在"); continue
    s = io.open(path).read()
    if "stub_status" in s:
        print(f"OK {path} 已有 status"); continue
    shutil.copy(path, BK + "/" + path.replace("/", "_"))
    block = ("\n  # 监控采集：本地 stub_status（活跃连接=并发、请求累计）\n"
             "  server {\n"
             f"    listen 127.0.0.1:{port};\n"
             "    server_name _;\n"
             "    location /stub_status { stub_status; access_log off; }\n"
             "  }\n")
    stripped = s.rstrip()
    assert stripped.endswith("}"), path + " 结尾不是 }"
    idx = stripped.rfind("}")
    out = stripped[:idx] + block + "}\n"
    io.open(path, "w").write(out)   # 原地覆写，inode 不变
    print(f"DONE {path} :{port}")
print("backup:", BK)
