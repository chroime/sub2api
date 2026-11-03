---
name: sub2api-billing-report
description: Export Sub2API per-account billing statistics to Excel for any time range. Use when the user asks for 扣费统计, 账户扣费, 用量报表, billing report, cost breakdown by account, how much a group or its accounts were charged, or wants an Excel of actual_cost / 实际扣费 per account for a given period.
---

# Sub2API 扣费报表

按账户导出扣费统计到 Excel。数据源是部署机 Postgres 里的 `usage_logs`，脚本通过 SSH 执行聚合查询。

## 前置：连接参数

全部来自环境变量，不要写进脚本或命令行历史。

```bash
export SUB2API_SSH_HOST='<部署机地址>'
export SUB2API_SSH_PORT='50002'          # 默认 22
export SUB2API_SSH_USER='root'           # 默认 root
export SUB2API_SSH_PASSWORD='<密码>'      # 或改用 SUB2API_SSH_KEY 指定私钥
```

可选（默认值适用于标准部署）：`SUB2API_PG_CONTAINER=sub2api-postgres`、`SUB2API_PG_USER=sub2api`、`SUB2API_PG_DB=sub2api`。

## 用法

```bash
python scripts/account_billing_report.py --group "Grok Ultra" --last 7d
```

### 时间区间

| 参数 | 含义 |
|---|---|
| `--last 7d` / `--last 24h` | 最近 N 天或 N 小时 |
| `--today` | 今天 00:00 至现在 |
| `--month` | 本月 1 日至现在 |
| `--start 2026-10-01 --end 2026-11-01` | 指定区间，含起始、不含结束 |
| 都不给 | 全部历史 |

时间按**部署机本地时区**解释。只给 `--start` 表示该时刻之后的全部，只给 `--end` 表示该时刻之前的全部。

### 统计口径

这一项决定金额，选错会差很多，务必和用户确认。

| 口径 | 含义 | 适用场景 |
|---|---|---|
| `--scope group`（默认） | 只统计**经该分组发起**的请求 | 算这个分组的收入 |
| `--scope account` | 统计该分组**绑定账号的全部扣费**，含其他分组 | 算这些账号的总成本 |

同一账号常挂在多个分组下（如 Grok Ultra 与 Grok Heavy）。`group` 口径不含其他分组的用量，`account` 口径含。实测同一批账号两种口径可相差数倍。

### 分组定位

`--group` 接 ID 或名称。名称支持模糊匹配，但匹配到多个且无精确同名时会列出候选并退出，不会猜。省略 `--group` 则统计全部分组。

```bash
python scripts/account_billing_report.py --group 92 --month          # 按 ID
python scripts/account_billing_report.py --group "Grok Heavy" --today # 按名称
python scripts/account_billing_report.py --last 7d                    # 全部分组
```

### 输出

默认按分组和时间命名，如 `Grok_Ultra_账户扣费_20261101_2340.xlsx`。用 `-o` 指定路径。

表格第 1 行写明分组、区间、口径，表头在第 3 行（已冻结并加筛选），末行为合计。列含：账户ID、名称、平台、状态、请求数、用户数、使用分组数、实际扣费、原价、四类 tokens、首末使用时间、备注。按实际扣费降序。

指定 `--group` 时，表里列出该分组**当前绑定的全部账号**，区间内没有用量的账号排在末尾，金额为 0，备注写明账号创建时间。所以表里的账号数等于分组后台显示的账号数，合计行会注明其中多少个有用量。

`account` 口径额外拆出两列：「<分组名> 扣费」和「其他分组扣费」，两者相加等于实际扣费，能看出共享账号被其他分组（如 Grok Heavy）用了多少。

## 字段说明

- **实际扣费** = `actual_cost`，用户真正被扣的金额，已含分组倍率等折扣
- **原价** = `total_cost`，未打折的金额，两者之比即实际折扣率
- **使用分组数**：该账号在统计区间内被几个分组用过，`account` 口径下大于 1 说明跨分组

## 注意

- 数据持续增长，不同时刻查同一区间，末尾时间点的数字会有小幅变化
- 账号绑定关系取**当前状态**。历史上曾绑定、现已解绑的账号，其历史用量在 `group` 口径下仍计入（日志记录的是当时的 group_id），但在 `account` 口径下不计入（按当前绑定取账号集合）
- 分组名、时间等用户输入走 psql 参数绑定，不拼接进 SQL
- 需要 `paramiko` 和 `openpyxl`
