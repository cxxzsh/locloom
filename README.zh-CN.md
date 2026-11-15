# LocLoom — 跨平台原位无损本地化工作台

[English](README.md) | [简体中文](README.zh-CN.md)

---

**LocLoom** 是一款基于 Go 与 [`egoist/mygo`](https://github.com/egoist/mygo) 构建的跨平台单文件桌面与命令行本地化工作台（`CGO_ENABLED=0`），支持 **macOS**、**Windows** 与 **Linux**。工具围绕“原位无损回填”设计，原生支持 **8 大主流本地化格式**（`Qt .ts`、`GNU Gettext .po/.pot`、`JSON i18n`、`Flutter .arb`、`Apple .xcstrings`、`Android strings.xml`、`.NET .resx`、`XLIFF 1.2/2.0`），通过十源并发瀑布流自动补齐译文、导出多模式 `.xlsx` 审校表，并**无损写回**目标文件。

## 1. 核心架构与能力

- **原生支持 8 大无损本地化格式**
  - **Qt Linguist (`.ts`)**：原位 UTF-8 字节偏移 XML 补丁，严格保留 BOM、CRLF/LF 换行、缩进、XML 注释、`<!DOCTYPE TS>`、`<location>`、`<byte value="x..."/>` 与 `<numerusform>` 复数形式。
  - **GNU Gettext (`.po` / `.pot`)**：完整保留文件头、译者/提取注释、`msgctxt` 上下文、`msgid_plural` / `msgstr[n]` 复数与 `fuzzy` 标记。
  - **JSON i18n (`.json`)**：支持嵌套与扁平键值对 JSON、占位符保护及源模板原文自动回填。
  - **Flutter ARB (`.arb`)**：完整保留 `@metadata` 元数据、ICU 占位符与 `@@locale`。
  - **Apple String Catalogs (`.xcstrings`)**：单文件多语种目录，保留 `sourceLanguage`、`extractionState`、`variations` 与 `state`。
  - **Android (`strings.xml`)**：支持 `values/strings.xml` 与 `values-<lang>/strings.xml`，兼容 `translatable="false"`、`<string-array>` 与 `<plurals>`。
  - **.NET (`.resx`)**：保留 XML schema 头、`resheader`、`<data>` 注释与非字符串资源节点。
  - **XLIFF (`.xliff` / `.xlf`)**：同时支持 XLIFF 1.2（`<trans-unit>`）与 XLIFF 2.0（`<unit>` / `<segment>`）。
- **上下文与键名安全去重**
  - 以 `(ID, Context, Source, Comment)` 四元组（`EntryKey`）唯一标识词条，不同上下文或键名下的相同原文互不串扰。
  - 针对 ID 键控格式（如 `en.json`、`values/strings.xml`、`Strings.resx`、`.pot`），自动从源模板回填可读原文供审校与机翻使用。
  - 默认严格隔离仅存在于 `.xlsx` 中的外部词条，仅在显式开启 **「追加词表新增词条」** 时才插入目标文件。
- **十源并发瀑布流引擎（10-Source Concurrent Waterfall）**
  - 支持拖拽自定义优先级链式回退补缺：**本地 `.xlsx` 词条表** → **本地翻译记忆库 (TM)** → **Google Web（免 Key）** → **Microsoft Bing / Edge（免 Key / Azure）** → **Lingva / LibreTranslate（免 Key）** → **MyMemory（免 Key）** → **DeepL / DeepLX** → **OpenAI / Ollama 兼容大模型** → **百度翻译开放平台** → **小牛翻译 Niutrans**。
  - 支持多语种并发调度、持久化本地缓存（`translation-cache.json`）以及 **HTTP / SOCKS5 代理**（支持自签证书网关跳过 TLS 校验）。
- **占位符与格式护栏**
  - 发送给远端翻译引擎前自动掩码 Qt（`%1`..`%99`、`%n`）、printf（`%s`、`%d`、`%1$s`）、ICU/模板（`{name}`、`{{var}}`）及前后空白；还原后自动校验一致性并标记 `[⚠]`。

## 2. 跨平台编译与命令行参数

### 编译命令（全平台 `CGO_ENABLED=0`）

```bash
# macOS (Apple Silicon / Intel)
CGO_ENABLED=0 go build -ldflags="-s -w" -o locloom .

# Windows (隐藏控制台窗口的 GUI 可执行文件)
CGO_ENABLED=0 go build -ldflags="-s -w -H=windowsgui" -o locloom.exe .

# Linux
CGO_ENABLED=0 go build -ldflags="-s -w" -o locloom .
```

### 桌面安装包打包（`mygo.json`）

```bash
# 构建各平台原生安装包（.dmg、NSIS setup .exe、.deb、.rpm、.tar.gz）
go tool mygo build -platform darwin/universal,windows/amd64,linux/amd64
```

社区预编译安装包暂未附带商业 Apple Developer 或 Authenticode 代码签名，首次运行时请按平台放行：

- **macOS (`.dmg`)**：将 `LocLoom.app` 拖入 `/Applications`（应用程序）后，若 Gatekeeper 提示「已损坏」或「无法验证开发者」，执行一次以下命令移除隔离属性后重新打开：

  ```bash
  sudo xattr -cr /Applications/LocLoom.app
  ```

  或前往 **系统设置 → 隐私与安全性** 点击 **仍要打开**。
- **Windows (`.exe` / `.zip`)**：若 SmartScreen 弹出「Windows 已保护你的电脑」，点击 **更多信息 → 仍要运行**。
- **Linux (`.deb` / `.rpm` / `.tar.gz`)**：可通过 `sudo apt install ./locloom_*_amd64.deb` 或 `sudo rpm -Uvh ./locloom-*.rpm` 安装，或直接解压便携压缩包运行。

### 命令行启动参数

| 参数 | 说明 | 示例 |
| :--- | :--- | :--- |
| `-src <path>` | 启动时自动加载的本地化目录或 `.zip` 压缩包 | `-src ./i18n/ts` |
| `-ts <path>` | `-src` 的兼容别名：启动时自动加载 `.ts` 目录或 `.zip` | `-ts ./i18n/ts` |
| `-xlsx <path>` | 启动时自动导入的 `.xlsx` 词条表 | `-xlsx ./glossary.xlsx` |
| `-theme <mode>` | 界面主题：`system`（默认）、`light`、`dark` | `-theme dark` |
| `-lang <mode>` | 界面语言覆盖：`zh`（默认）、`en`、`auto`（跟随系统） | `-lang en` |
| `-open-help` | 启动后自动打开内置帮助弹窗 | `-open-help` |
| `-headless-render <png>` | 无头渲染一帧 UI 到指定 PNG 并退出（用于烟雾验证） | `-headless-render ./preview.png` |
| `-width <px>` | 初始窗口宽度（默认 `1280`） | `-width 1440` |
| `-height <px>` | 初始窗口高度（默认 `820`） | `-height 900` |

```bash
# macOS / Linux 示例
./locloom -src ./translations -xlsx ./translations.xlsx -theme dark

# Windows PowerShell 示例
.\locloom.exe -src .\translations -xlsx .\translations.xlsx -theme system
```

## 3. 标准工作流与状态标记

1. **加载任意格式项目**：点击顶部工具栏 **「打开目录」** / **「打开文件」**（或直接拖入单个/多个文件、目录或 `.zip`）。工作台自动识别格式并在顶部显示格式徽章，同时通过 **智能语种同步** 自动对齐当前项目包含的目标语种。
2. **导入 / 导出 `.xlsx` 词条表**：拖入 `.xlsx` 或通过顶部按钮导入导出，支持 `Key ID`、`Context`、`Source`、`Comment` 及多语种列（表头含 `参考` / `ref` 的列默认跳过）。支持导出 **全部词条**、**仅未翻译**、**仅待写入变更** 与 **仅含告警**。
3. **配置翻译源与代理**：在 **「管线」** 选项卡点击展开任意翻译源手风琴卡片，支持密钥明文切换、连通性测试、LLM 模型自动发现与 HTTP/SOCKS5 代理配置。
4. **单行快翻、缺口跳转与安全写回**：支持 **聚焦单语种** 与 **全景矩阵**（点击任意语种单元格即可直达编辑），底部检查栏提供 **⚡ 翻译此条**、**◀ 上一待办 / 下一待办 ▶**、**复制原文 / 填入原文 / 重置该格** 与格式感知的 Before/After 差异预览；点击 **「写回保存 (N)」** 即可无损落盘（支持 `.bak` 备份与 `*_updated.zip`）。
5. **中英文实时切换**：点击顶部工具栏右上角 **`[EN]` / `[中文]`** 按钮，即可实时切换全界面语言；也可通过 `-lang <auto|zh|en>` 启动参数指定。

单元格状态前缀仅用于界面提示，**绝不会写入本地化文件或 `.xlsx`**：

| 标记 | 含义 | 说明 |
| :--- | :--- | :--- |
| **`[补]`** | 补缺写入 | 原 `.ts` 为 `unfinished` 或空译文，写入时填入新译文并移除 `type="unfinished"` |
| **`[改]`** | 覆盖已有 | 仅在「覆盖已有」模式下出现，替换原 `.ts` 中已完成的旧译文 |
| **`[新]` / `[加]`** | 追加新词条 | 仅在勾选「追加缺失」时出现，将 `.ts` 中不存在的词条追加到目标 `<context>` |
| **`[⚠]`** | 格式告警 | `%1` / `%n` 占位符、掩码标记或末尾 `\t` 与原文不一致 |

## 4. 配置文件与缓存路径

LocLoom 自动遵循各操作系统的标准目录规范，并支持**便携模式（Portable Mode）**：

| 平台 | 配置文件路径 (`config.json`) | 缓存文件路径 (`translation-cache.json`) |
| :--- | :--- | :--- |
| **macOS** | `~/Library/Application Support/locloom/config.json` | `~/Library/Caches/locloom/translation-cache.json` |
| **Windows** | `%AppData%\locloom\config.json` | `%LocalAppData%\locloom\translation-cache.json` |
| **Linux** | `~/.config/locloom/config.json` | `~/.cache/locloom/translation-cache.json` |
| **便携模式** | 若可执行文件同级目录已存在 `config.json`，优先读写同级目录文件 |

## 5. 多格式扩展架构说明

LocLoom 通过 `internal/format` 统一驱动注册表将领域模型（`internal/domain`）、矩阵规划器（`internal/planner`）与桌面工作台（`internal/app`）解耦于具体文件语法，在保持 `CGO_ENABLED=0` 零依赖跨平台编译的同时实现 8 大本地化生态的原位无损往返。
