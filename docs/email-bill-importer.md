# 邮箱账单自动导入

该功能已直接集成进 ezBookkeeping。服务进程通过 IMAPS 读取邮件，使用现有 cron 调度，在主数据库里创建原生交易；不需要 Python 解析器、API Token、第二个容器或额外状态库。

当前支持参考 `../billCheckFormMail` 实际启用的两类招商银行邮件：

- 信用卡“每日信用管家”消费与退款
- 储蓄卡动账通知的支出、入账和资金归集

## 配置

先正常启动 ezBookkeeping、创建用户以及对应的招行信用卡账户、储蓄卡账户、支出分类和收入分类。随后编辑 `conf/ezbookkeeping.ini` 的 `[email_bill]`：

```ini
[email_bill]
enabled = true
target_user = alice
imap_server =
imap_port = 993
mail_user = your-account@qq.com
mail_password = your-imap-app-password
cmb_credit_account_id = 101
cmb_debit_account_id = 102
expense_category_id = 201
income_category_id = 202
timezone = Asia/Shanghai
cron_expression = 0 3 * * *
max_emails = 60
require_authentication_results = true
trusted_authserv_domains = qq.com
```

`mail_password` 应填写邮箱的 IMAP 应用授权码，不要使用网页登录密码。QQ、Foxmail、163、126、Yeah、Gmail、Outlook 和 Hotmail 可根据 `mail_user` 自动推断 `imap_server`；其他邮箱必须明确填写。

`cron_expression` 使用五段 cron 格式：`分钟 小时 日 月 星期`，并按 `timezone` 执行。例如：

- `0 3 * * *`：每天 03:00
- `30 8 * * 1-5`：周一至周五 08:30
- `0 6 * * 1`：每周一 06:00

账户和分类 ID 必须属于 `target_user`。如需使用仓库内工具查询 ID，可临时在 `[security]` 开启 API Token 后执行：

```bash
export EBKTOOL_SERVER_BASEURL=http://localhost:8080
export EBKTOOL_TOKEN='你的 API Token'
sh skills/ezbookkeeping/scripts/ebktools.sh accounts-list
sh skills/ezbookkeeping/scripts/ebktools.sh transaction-categories-list
```

所有配置也支持项目原有的环境变量覆盖规则，例如 `EBK_EMAIL_BILL_ENABLED`、`EBK_EMAIL_BILL_CRON_EXPRESSION`、`EBK_EMAIL_BILL_MAIL_PASSWORD`。敏感值还可以使用文件变量，例如 `EBKCFP_EMAIL_BILL_MAIL_PASSWORD=/run/secrets/mail_password`。

## 运行

仓库根目录的 Compose 只有一个服务：

```bash
docker compose pull
docker compose up -d
```

修改配置后执行 `docker compose restart ezbookkeeping`。查看导入日志：

```bash
docker compose logs -f ezbookkeeping
```

立即执行一次导入：

```bash
docker compose exec ezbookkeeping ./ezbookkeeping cron run --name ImportEmailBills
```

每笔交易的备注包含 `[ebk-mail:...]` 标记。后续轮询直接在 ezBookkeeping 主数据库中检查该标记，因此容器重启或重复收到同一封邮件都不会重复入账；用户主动删除的带标记交易也不会被自动建回。

默认要求收件服务器的首个 `Authentication-Results` 来自 `trusted_authserv_domains`，并且其中招行域名的 SPF 或 DKIM 结果为通过。自建邮箱应明确设置可信认证服务域名；只有确认邮件链路可信但服务商不提供该头时，才考虑关闭 `require_authentication_results`。

数据直接保存在 `./data`，附件保存在 `./storage`。请定期备份这两个目录和 `conf/ezbookkeeping.ini`，不要把含邮箱授权码的配置提交到公开仓库。
