# 灵台 lingtai

> *美猴王一见，倒身下拜，磕头不计其数，口中只道：「师父，师父，我弟子志心朝礼，志心朝礼。」*
> *祖师道：「你从那方来的？且说个乡贯姓名明白，再拜。」*
> — 西游记·第一回，悟空至灵台方寸山

智能体框架 — 灵台方寸山，悟空学七十二变的地方。

## 灵台是什么

灵台方寸山，斜月三星洞。悟空在这里从一只猴子变成了齐天大圣——不是因为山本身有什么魔力，而是因为这里提供了修行所需的一切：师父（LLM）、功法（能力）、同门（其他智能体）、以及一个可以安心修炼的地方（工作目录）。

灵台做的事情也是这样：给智能体一个修行的地方，让它学会七十二变。

## 两层架构

| 层 | 包 | 灵台的含义 |
|----|-----|-----------|
| 内核 | `lingtai-kernel` | **心** — 最小的内在，承载灵魂但不自知 |
| 框架 | `lingtai` | **灵台方寸山** — 修行之地，能力从这里习得 |

内核只提供"心"——思考、通信、记忆。一颗朴素的心。

灵台在心的基础上，赋予智能体能力：读写文件、搜索网络、观察图像、生成语音、化出分身……就像悟空在灵台方寸山学到的本领。

## 安装

```bash
pip install lingtai                  # 完整框架
pip install lingtai-kernel           # 仅内核
```

可选依赖（按 LLM 供应商）：
```bash
pip install lingtai[gemini]          # Google Gemini
pip install lingtai[openai]          # OpenAI / DeepSeek / Grok
pip install lingtai[anthropic]       # Claude
pip install lingtai[minimax]         # MiniMax
pip install lingtai[all]             # 全部
```

## 快速开始

```python
from lingtai import Agent
from lingtai.llm import LLMService

# 创建 LLM 服务
service = LLMService(provider="gemini", model="gemini-2.5-flash")

# 创建智能体——给它一个灵台（工作目录）
agent = Agent(
    service=service,
    working_dir="/agents/wukong",
    agent_name="悟空",
    capabilities=["file", "vision", "web_search", "bash"],
)

agent.start()
agent.send("你好，师父")
agent.stop()
```

## 能力（七十二变）

| 能力 | 用途 |
|------|------|
| `file` | 读、写、编辑、搜索文件（展开为 read/write/edit/glob/grep） |
| `psyche` | 进化的身份、知识库、记忆管理 |
| `bash` | 执行 shell 命令 |
| `avatar` | 化出分身——生成子智能体 |
| `email` | 完整的邮件客户端（回复、联系人、定时发送） |
| `vision` | 图像理解 |
| `web_search` | 搜索网络 |
| `web_read` | 读取网页 |
| `talk` | 文字转语音 |
| `compose` | 生成音乐 |
| `draw` | 文字转图像 |
| `listen` | 语音转文字、音乐分析 |
| `library` | 知识归档与检索 |

## 四种内置工具（固有之器）

这些来自内核，每个智能体天生就有：

| 工具 | 用途 |
|------|------|
| `mail` | 信箱——智能体之间的通信 |
| `system` | 生命周期管理——查看状态、小憩、刷新 |
| `eigen` | 记忆和身份——编辑记忆、凝蜕（上下文重置）、设真名 |
| `soul` | 内心的声音——自省、探问 |

## 智能体就是一个文件夹

每个智能体的身份就是它的工作目录路径。没有 agent_id，路径即身份：

```
/agents/wukong/                ← 这个路径就是悟空
  .agent.lock                  ← 独占锁
  .agent.heartbeat             ← 心跳（存活证明）
  .agent.json                  ← 清单
  system/
    covenant.md                ← 盟约（受保护的指令）
    memory.md                  ← 记忆
  mailbox/                     ← 信箱
  logs/                        ← 日志
```

智能体之间通过文件系统信件通信——写入对方的 `mailbox/inbox/`。就像传书。

## 扩展方式

```python
# 用能力组合
agent = Agent(
    service=service,
    working_dir="/agents/bajie",
    capabilities=["file", "bash", "email"],
)

# 或者自定义子类
class ResearchAgent(Agent):
    def __init__(self, **kwargs):
        super().__init__(
            capabilities=["file", "vision", "web_search"],
            **kwargs,
        )
        self.add_tool("query_db", schema={...}, handler=db_handler)
```

## 许可

MIT
