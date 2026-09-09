# 邮件自动记账

该功能直接运行在 ezBookkeeping 内部，共用现有用户、分类、账户、数据库、LLM 配置和交易服务；不需要额外解析器、API Token、容器或状态库。

## 启动

仓库根目录的 Compose 只有一个服务，数据统一保存在 `./data`：

```yaml
services:
  ezbookkeeping:
    image: ghcr.io/yaho7/ezbookkeeping:latest
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - ./data:/data
```

```bash
docker compose pull
docker compose up -d
```

容器入口会建立 `/data/log` 和 `/data/storage`，并使用可写的数据目录启动。数据库、附件、日志和配置都位于 `./data`，备份这个目录即可。

## 在界面配置

登录后打开 **设置 → 邮件自动记账**：

1. 在“邮箱与计划”填写 IMAP 服务器、邮箱账号和应用授权码。
   可先点击“测试连接”，只验证 TLS、登录和只读打开收件箱，不会拉取邮件或入账。
2. 选择每天、每周某天和具体时间；复杂计划可切换到五段 Cron。
3. 新建一条或多条解析规则。所有命中的规则会各自在受限 Starlark 沙箱运行，任意一条失败不会阻断其他规则。
4. 建立账户路由，例如按银行、卡类型、卡号末四位或币种映射到 ezBookkeeping 账户。
5. 建立商户分类规则。系统先匹配规则，未命中时才调用 ezBookkeeping 已配置的 LLM；高置信度的新分类会被创建，并留下可编辑、可停用的学习规则。
6. 在“审核与审计”处理冲突或低置信度结果，并查看“邮件 → 解析规则 → 分类规则/LLM → 账户路由 → 交易”的完整链路。

可以点击“立即执行”手动拉取，也可以等待保存的计划执行。

每次运行会只读扫描所有可读取的邮件文件夹，包括自建及嵌套的“账单”文件夹；仅作为目录、不能打开的父文件夹会跳过。先扫描自建文件夹，再扫描收件箱和常见系统文件夹，各文件夹共用“每次最大邮件数”的上限。同一封邮件被移动或复制到其他文件夹后仍按邮件身份去重，不会因文件夹不同而重复入账。

邮件按每批最多 50 封读取必要的邮件头，再下载通过筛选的正文，避免一次读取整个邮箱触发 IMAP 超时。邮件任务无论成功或失败都会释放运行锁；任务执行期间仍会阻止重复启动，结束后可以立即再次运行。

系统会自动选择邮件认证策略：QQ/Foxmail、163、Gmail、Outlook 等已知邮箱会校验其 SPF/DKIM 认证结果；无法识别的小众或自建 IMAP 不强制该认证，仍按解析规则的发件人和主题筛选。

### 全局 AI 设置

语言模型不在邮件自动记账中单独配置。打开 **设置 → 应用设置 → AI 设置**，选择供应商并填写模型、API 地址和密钥。这里保存的是 ezBookkeeping 全局文本识别模型，原有 AI 功能、账单分类回退以及解析规则生成共用这份配置。

密钥不会通过读取接口返回；页面留空保存会保留现有密钥。要停用全局 AI，将供应商改为“禁用”并保存。

## 解析规则与测试台

解析规则编辑器可以选择一封已保留的邮件，也可以直接粘贴发件人、主题和正文。点击“使用 AI 生成”后，服务端会调用全局 AI 设置，生成一份未保存的规则草稿，并立即在受限沙箱中运行：只有规则能匹配样本且至少解析出一笔标准账单时，草稿才会填入编辑器。用户仍需检查预览并手动保存，生成操作不会入账。

### 解析器 API

解析代码使用 Starlark，必须定义：

```python
def parse(mail):
    return []
```

`mail` 是只读字典：

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `message_id` | string | 邮件 Message-ID |
| `sender` | string | 发件人 |
| `subject` | string | 主题 |
| `received_at` | string | RFC3339 收件时间 |
| `text` | string | 纯文本正文 |
| `headers` | dict | 已允许传入的邮件头，键为小写 |

沙箱只提供三个纯函数：

- `regex_find(text, pattern)`：返回第一个捕获组；没有捕获组时返回完整匹配；未命中返回空字符串。
- `sha256(value)`：返回小写十六进制 SHA-256。
- `parse_builtin(name, mail)`：调用维护在 ezBookkeeping 内的解析器；目前支持 `cmb_credit` 和 `cmb_debit`。

每笔标准账单是一个字典，必填字段为：

- `occurred_at`：RFC3339 时间；
- `amount`：正数十进制字符串，例如 `"38.50"`；
- `currency`：三字母币种，例如 `"CNY"`；
- `flow_type`：`expense`、`income`、`refund`、`transfer_in` 或 `transfer_out`。

可选字段为 `external_id`、`merchant`、`description`，以及 `account_hint` 字典中的 `bank`、`kind`、`last4`。解析器不能导入模块，也不能访问网络、文件、数据库、环境变量或 ezBookkeeping 内部服务。

招行信用卡预置规则示例：

```python
def parse(mail):
    return parse_builtin("cmb_credit", mail)
```

通用规则示例：

```python
def parse(mail):
    amount = regex_find(mail["text"], r"金额[:：]\s*([0-9]+(?:\.[0-9]{1,2})?)")
    if not amount:
        return []
    return [{
        "external_id": sha256(mail["message_id"] + amount),
        "occurred_at": mail["received_at"],
        "amount": amount,
        "currency": "CNY",
        "flow_type": "expense",
        "merchant": "待确认商户",
        "description": mail["subject"],
        "account_hint": {"bank": "example", "kind": "debit"},
    }]
```

### HTTP 接口

所有接口都需要当前 ezBookkeeping 登录凭据：

- `POST /api/v1/email_bill/parsers/test.json`：测试 matcher、sourceCode 和 mail，不保存、不入账。
- `POST /api/v1/email_bill/parsers/generate.json`：使用全局 AI 从 mail 生成草稿，服务端自测后返回。
- `POST /api/v1/email_bill/parsers/save.json`：用户确认后保存规则新版本。

生成请求示例：

```json
{
  "mail": {
    "messageId": "<sample@example.com>",
    "sender": "notice@bank.example",
    "subject": "交易通知",
    "receivedAt": "2026-09-09T08:30:00+08:00",
    "text": "您于09月09日消费人民币38.50元",
    "headers": {}
  }
}
```

设置页的“解析器接口与 AI 提示词”会根据当前邮件样本生成可复制的完整提示词，可直接交给外部 AI；返回的 `sourceCode` 仍应先放入测试台验证。

解析代码必须定义 `parse(mail)`，返回标准账单列表。示例：

```python
def parse(mail):
    if "消费" not in mail["subject"]:
        return []
    return [{
        "external_id": "bank-order-id",
        "occurred_at": "2026-09-09T08:30:00+08:00",
        "amount": "28.50",
        "currency": "CNY",
        "flow_type": "expense",
        "merchant": "示例商户",
        "description": mail["subject"],
        "account_hint": {
            "bank": "example-bank",
            "kind": "credit",
            "last4": "1234",
        },
    }]
```

沙箱不提供文件、网络、进程、环境变量、数据库或模块加载能力，并限制执行时间、指令数和输出数量。测试台可以直接粘贴邮件，也可以选择已保留的邮件；测试只展示标准账单，不会路由、调用 LLM、创建分类或入账。

## 隐私与去重

系统默认只保存正文哈希和摘要。只有显式开启“为测试台保留邮件原文”后才保存正文，并按设置的天数在任务运行前清理；密码不会返回浏览器，审计事件也不保存密码、Token、完整认证头或邮件原文。

邮件身份、候选账单、解析器版本和交易导入意图都有独立唯一键。同一封邮件重复拉取不会重复解析，同一候选无论重试或并发执行都只会绑定一笔原生交易。解析冲突、账户未命中、LLM 低置信度和导入失败会保留为可审核状态。

每次只下载并识别“每次最大邮件数”指定数量的未处理邮件，默认 50 封。已处理的 Message-ID 会在下载正文前跳过，因此积压邮件可通过多次“立即执行”或计划任务逐批消化，不需要把单次上限调到 500 或 1000。

## Action 与镜像

`.github/workflows/docker-publish.yml` 在每次 push 后构建 amd64 和 arm64 镜像，先发布提交不可变标签，再在确认提交仍是分支最新提交时更新分支标签及 `main` 的 `latest`。工作流不会修改仓库或产生新提交，因此不会形成 Action 自触发循环。
