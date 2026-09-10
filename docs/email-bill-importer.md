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

可以点击“立即执行”手动拉取，也可以等待保存的计划执行。运行接口只创建后台任务并立即返回任务信息，不再等待全部邮件扫描完成；重复点击会返回正在执行的任务。运行中可以修改设置或离开页面，当前任务继续使用启动时的配置，新的设置用于下一次任务。

在“邮箱与计划”点击“读取文件夹”，仅登录邮箱并获取可读取目录，不读取邮件头或正文。勾选“只扫描指定文件夹”，选择例如 `其他文件夹/账单` 并保存后，手动和定时任务都只扫描选中的目录，不自动包含其子目录。目录被删除或重命名时，任务会报错提示重新读取目录，不会退回全量扫描。未保存目录选择时，不能启动新任务。

旧配置保持“全部文件夹”行为，也可以主动选回这个选项。全量模式包括自建及嵌套文件夹，仅作为目录、不能打开的父文件夹会跳过。先扫描自建文件夹，再扫描收件箱和常见系统文件夹，各文件夹共用“每次最大邮件数”的上限。同一封邮件被移动或复制到其他文件夹后仍按邮件身份去重，不会因文件夹不同而重复入账。

邮件按每批最多 50 封读取必要的邮件头，再下载通过筛选的正文。每批下载后立即解析并保存结果，后续文件夹连接失败不会丢失已完成批次的结果。各 IMAP 命令有超时，整个后台任务最长运行 30 分钟；达到时限后可再次运行以继续处理未导入的邮件。

后台协调器适用于单个 ezBookkeeping 服务实例。手动和定时任务共用同一运行锁，任务结束后可以立即再次运行。服务重启后，下次读取状态或启动任务会把未完成任务标记为“已中断”，同时更新尚在下载或处理中的邮件状态；重启不会自动重放未完成任务。

系统会自动选择邮件认证策略：QQ/Foxmail、163、Gmail、Outlook 等已知邮箱会校验其 SPF/DKIM 认证结果；无法识别的小众或自建 IMAP 不强制该认证，仍按解析规则的发件人和主题筛选。

QQ/Foxmail 可能在 `Authentication-Results` 的域名或邮件地址中间折行，例如 `message.c` 与 `mbchina.com`。认证解析会在单个属性内还原折行，同时继续校验可信认证服务器和招行域名；不会因为出现 `pass` 字样就跳过域名验证。

### 邮件工作台与排查

设置页默认打开“邮件工作台”，按文件夹、邮件列表、邮件详情展示扫描结果。顶部显示任务状态、当前文件夹、扫描/下载/处理/跳过/失败数量和最近更新时间。运行时约每 3 秒刷新，空闲时约每 15 秒检查定时任务；离开页面停止轮询，后台任务继续运行。

邮件列表可按发件人/主题搜索、文件夹和处理状态筛选，每页 50 封。列表保留扫描历史，远端移动或删除邮件不会删除本地扫描证据。认证失败、没有匹配规则、重复邮件、超过大小限制和下载错误也会显示，不再静默跳过。扫描索引只保存元数据，不包含密码或正文。

选择邮件后可以查看处理原因、保留的纯文本正文、解析器版本、输出数量、解析错误和候选账单状态。正文未保留、已过保留期，或邮件未通过认证/规则筛选时，页面会说明正文不可用。有正文的邮件可直接送入现有测试台，测试本身不入账。

“扫描完成”不代表已经入账，“已处理”统计的是邮件数量。查看详情中的候选账单状态：如果显示“等待指定账户”，先配置账户路由或进入“审核与审计”指定账户和分类；只有“已入账”才表示创建了交易。已处理的邮件不会因重新扫描而再次解析；已有候选的账户、分类和导入问题通过审核或重试处理。

旧版把整次扫描放在 HTTP 请求中，可能出现浏览器 524 后服务端仍继续扫描的情况。新版运行接口返回任务 ID，实际失败原因通过任务状态和邮件详情读取，无需延长浏览器或代理的等待时间。

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

- `POST /api/v1/users/settings/email_bill/run.json`：提交后台任务，返回任务对象；重复请求返回同一正在运行的任务。
- `POST /api/v1/users/settings/email_bill/folders.json`：使用表单邮箱连接信息读取目录；密码留空沿用已保存密码，不保存设置或导入邮件。
- `GET /api/v1/users/settings/email_bill/status.json`：返回当前用户最新任务及文件夹进度，无任务时返回 `null`。
- `GET /api/v1/email_bill/mailbox/list.json`：读取扫描记录，支持 `folder`、`status`、`search`、`page`，返回每页最多 50 封及总数。
- `GET /api/v1/email_bill/mailbox/detail.json?id=...`：读取当前用户拥有的扫描邮件及关联处理证据；最多展示最近一次处理中的 100 个解析执行和 100 条候选账单。
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

任务和扫描索引通过启动时的增量建表保存到现有用户数据库，不替换原有交易或审计表。邮件详情仅以文本呈现，不执行邮件中的 HTML 或脚本。

邮件身份、候选账单、解析器版本和交易导入意图都有独立唯一键。同一封邮件重复拉取不会重复解析，同一候选无论重试或并发执行都只会绑定一笔原生交易。解析冲突、账户未命中、LLM 低置信度和导入失败会保留为可审核状态。

每次只下载并识别“每次最大邮件数”指定数量的未处理邮件，默认 50 封。已处理的 Message-ID 会在下载正文前跳过，因此积压邮件可通过多次“立即执行”或计划任务逐批消化，不需要把单次上限调到 500 或 1000。

## Action 与镜像

`.github/workflows/docker-publish.yml` 在每次 push 后构建 amd64 和 arm64 镜像，先发布提交不可变标签，再在确认提交仍是分支最新提交时更新分支标签及 `main` 的 `latest`。工作流不会修改仓库或产生新提交，因此不会形成 Action 自触发循环。
