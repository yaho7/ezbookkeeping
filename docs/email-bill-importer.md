# 邮箱账单自动导入

本 fork 增加了一个独立的小型导入器，参考同级目录 `../billCheckFormMail` 当前实际启用的解析链路，将招商银行邮件账单直接写入 ezBookkeeping，不再生成或提交 `bills.csv`。

## 当前范围

- 招商银行信用卡“每日信用管家”邮件
- 招商银行储蓄卡动账通知邮件
- 在服务端按招行发件人和主题筛选，并默认要求邮箱服务商写入的首个 `Authentication-Results` 表明招行域名通过 SPF 或 DKIM
- 支出、退款和入账分别映射为 ezBookkeeping 支出或收入
- SQLite 保存邮件和逐笔同步状态，容器重启后继续未完成任务
- 多个导入器意外同时运行时，通过 SQLite 租约避免同时发送同一笔待同步记录；解析异常的邮件会在下一轮重试
- 每笔备注包含 `ebk-mail:...` 幂等标记；即使 API 已成功但本地状态尚未写回，下一轮也会先查询远端，避免重复入账

参考项目 README 中提到的支付宝、微信和建行解析器并未在其当前 `main.py` 中启用，因此本次没有把文档中的历史描述当成现有能力复制。Notion、Server 酱和独立 CSV 也没有保留，因为 ezBookkeeping 已经是本改造的主数据与查看界面。

## 首次配置

1. 创建本地配置：

   ```bash
   cp .env.example .env
   openssl rand -hex 32
   ```

   把第二条命令的输出写入 `.env` 的 `EBK_SECURITY_SECRET_KEY`，并填写邮箱账号、IMAP 授权码。`.env` 已被 Git 忽略，不能提交任何真实密码或令牌。

2. 先只启动 ezBookkeeping：

   ```bash
   docker compose up -d ezbookkeeping
   ```

3. 浏览器打开 `http://服务器地址:8080`，完成账户初始化。在“用户设置 → 安全”中生成 API 令牌，并写入 `.env` 的 `EBK_API_TOKEN`。

4. 查询账户和分类 ID：

   ```bash
   export EBKTOOL_SERVER_BASEURL=http://localhost:8080
   export EBKTOOL_TOKEN='刚生成的 API 令牌'
   sh skills/ezbookkeeping/scripts/ebktools.sh accounts-list
   sh skills/ezbookkeeping/scripts/ebktools.sh transaction-categories-list
   ```

   将对应数字 ID 填入：

   - `CMB_CREDIT_ACCOUNT_ID`：招行信用卡账户
   - `CMB_DEBIT_ACCOUNT_ID`：招行储蓄卡账户
   - `EXPENSE_CATEGORY_ID`：邮件支出的默认分类
   - `INCOME_CATEGORY_ID`：退款和入账的默认分类

5. 启动完整服务：

   ```bash
   docker compose up -d
   ```

## 日常操作

查看导入日志：

```bash
docker compose logs -f email-bill-importer
```

立即重跑导入器：

```bash
docker compose restart email-bill-importer
```

默认每小时按受支持的发件人和主题组合检查最近 60 封匹配邮件。可通过 `.env` 的 `POLL_INTERVAL_SECONDS` 与 `MAX_EMAILS` 调整。首次导入前如果邮箱历史邮件很多，应适当提高 `MAX_EMAILS`。

默认的 `REQUIRE_AUTHENTICATION_RESULTS=true` 会读取邮箱服务商添加在最前面的认证结果，降低伪造 `From` 地址造成错误入账的风险。若服务商没有提供该邮件头，日志会显示邮件因未通过认证而被拒绝；确认邮箱链路可信后，才可把该值改为 `false`。导入器自身不执行完整 DKIM 密码学验证。

持久化数据位于三个 Compose 命名卷：

- `ezbookkeeping-data`：主数据库
- `ezbookkeeping-storage`：附件
- `email-bill-importer-data`：邮件指纹、待同步记录、远端交易 ID 与最近错误

删除或重建容器不会清除命名卷。不要在未备份时执行 `docker compose down -v`。

## 单次执行与排错

临时单次执行可使用：

```bash
docker compose run --rm -e RUN_ONCE=true email-bill-importer
```

常见错误：

- `missing required environment variables`：`.env` 仍有空值或变量未填写。
- `cannot reach ezBookkeeping`：主服务未健康、API 地址被手工覆盖，或容器网络异常。
- `api token is not enabled`：确认 Compose 中保留了 `EBK_SECURITY_ENABLE_API_TOKEN=true` 并重建主服务。
- `ezBookkeeping API rejected request`：令牌过期，或账户/分类 ID 不属于该令牌对应的用户。
- IMAP 登录失败：确认邮箱已启用 IMAP，且 `MAIL_PASS` 使用应用授权码。
- 日志出现 `rejected mail without passing bank authentication results`：邮箱服务商没有提供可识别的招行 SPF/DKIM 结果；优先检查原始邮件头，确认链路可信后才考虑关闭 `REQUIRE_AUTHENTICATION_RESULTS`。
