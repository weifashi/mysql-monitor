# 采集端部署脚本

ops-sentinel 服务端之外、装在被监控机器上的部分。全部走 node_exporter
的 textfile 采集器，零常驻内存（systemd timer 每分钟一跑）。

| 文件 | 装到哪 | 作用 |
|---|---|---|
| `ttpos-container-metrics.sh` | 各 VM `/usr/local/bin/` | 容器 cgroup 内存用量/上限/OOM 计数 |
| `ttpos-log-metrics.sh` | 各 VM `/usr/local/bin/` | 容器日志 error/fatal/panic 计数（增量扫描） |
| `ttpos-log-exclude.txt` | 各 VM `/var/lib/ttpos-log-metrics/exclude.txt` | 17 条业务噪音排除清单（源自生产 Cloud Logging 配置） |
| `install-log-metrics.sh` | 临时 | 单机安装器（脚本 + timer） |
| `rollout-log-metrics.sh` | 物理机上执行 | 下载一次、`incus file push` 到本机全部 VM 并安装 |
| `ttpos-log-samples.sh` | 各 VM `/usr/local/bin/` | 错误样本端口 `:9101` 的处理端（socket activation，零常驻） |
| `install-log-samples.sh` | 临时 | 装样本端口（socket + service 模板） |
| `allow-9101.sh` | 物理机上执行 | 给加固 VM 放行 9101（runtime 增量加 + conf 持久化**不重载**） |

## 已知约束

- rehearsal-db-* / storage-* 的 nftables output 链是 policy drop，
  **VM 内拉不到外网文件**——所以 rollout 脚本在物理机下载后用
  `incus file push` 推进去，不要改成 VM 内 curl。
- 告警附带错误样本的链路：扫描脚本把命中行留存到
  `/var/lib/ttpos-log-metrics/recent.log`（80 行窗口），`:9101` 按需吐出，
  ops-sentinel 的规则 `diag_url` 指向它，通知发送前拉取并附进消息。
- **给加固 VM 改防火墙绝不要整体 `nft -f` 重载**——配置文件首行的
  `flush ruleset` 会连 Docker 的 NAT 表一起清掉（详见 allow-9101.sh 头注）。
- node_exporter 需已启用 `--collector.textfile.directory=/var/lib/node_exporter/textfile`
  （容器指标部署时已在 20 台 VM 统一开启）。
