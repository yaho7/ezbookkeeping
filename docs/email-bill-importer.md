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
2. 选择每天、每周某天和具体时间；复杂计划可切换到五段 Cron。
3. 新建一条或多条解析规则。所有命中的规则会各自在受限 Starlark 沙箱运行，任意一条失败不会阻断其他规则。
4. 建立账户路由，例如按银行、卡类型、卡号末四位或币种映射到 ezBookkeeping 账户。
5. 建立商户分类规则。系统先匹配规则，未命中时才调用 ezBookkeeping 已配置的 LLM；高置信度的新分类会被创建，并留下可编辑、可停用的学习规则。
6. 在“审核与审计”处理冲突或低置信度结果，并查看“邮件 → 解析规则 → 分类规则/LLM → 账户路由 → 交易”的完整链路。

可以点击“立即执行”手动拉取，也可以等待保存的计划执行。

## 解析规则与测试台

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

## Action 与镜像

`.github/workflows/docker-publish.yml` 在每次 push 后构建 amd64 和 arm64 镜像，先发布提交不可变标签，再在确认提交仍是分支最新提交时更新分支标签及 `main` 的 `latest`。工作流不会修改仓库或产生新提交，因此不会形成 Action 自触发循环。
