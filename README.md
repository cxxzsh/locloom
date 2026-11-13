# LocLoom — Cross-Platform Lossless Localization Workbench

[English](#english) | [简体中文](#简体中文)

---

<a id="english"></a>

## English

**LocLoom** is a cross-platform, single-binary desktop and CLI localization workbench (`CGO_ENABLED=0`) built with Go and [`egoist/mygo`](https://github.com/egoist/mygo), supporting **macOS**, **Windows**, and **Linux**. Designed around lossless file preservation, it extracts localization entries (currently Qt Linguist `.ts` files and `.zip` archives), fills multi-language translations via a 4-stage concurrent waterfall pipeline, exports multi-mode `.xlsx` review workbooks, and patches target files back using **surgical byte-offset updates**.

### 1. Overview & Core Capabilities

- **Surgical Byte-Offset XML Patching**
  - Instead of full-document XML re-serialization, LocLoom computes exact UTF-8 byte offsets for `<translation>` nodes inside each `<message>`.
  - Preserves original **UTF-8 BOM**, **CRLF / LF line endings**, indentation, XML comments, `<!DOCTYPE TS>`, `<location>` tags, `<byte value="x..."/>` control characters, and `<numerusform>` plural forms verbatim.
  - Escapes only `&`, `<`, and `>` per Qt `lupdate` conventions (preventing double-unescape bugs) and validates the patched XML in memory with `encoding/xml` before writing to disk.
- **Context-Safe Disambiguation & Phantom-Row Protection**
  - Uniquely identifies entries by the `(Context, Source, Comment)` tuple (`EntryKey`), ensuring identical `<source>` strings in different `<context>` blocks (e.g., `Open` in `FileMenu` vs. `ConnectionState`) never leak across contexts.
  - Isolates `.xlsx`-only rows by default to prevent injecting phantom entries into `.ts` files; external rows are only inserted when **Append Missing** is explicitly enabled.
- **4-Stage Concurrent Waterfall Pipeline**
  - Falls back across providers in priority order: **Local `.xlsx` Glossary** → **Local `.ts` Translation Memory (TM)** → **Google Translate (Web, keyless batch)** → **OpenAI / Ollama-Compatible LLMs**.
  - Supports multi-locale concurrent scheduling, persistent local caching (`translation-cache.json`), and **HTTP / SOCKS5 proxies** (with optional TLS verification skip for self-signed gateways).
- **Qt Placeholder & Whitespace Guard**
  - Automatically masks `%1`..`%99`, `%L1`..`%L99`, `%n`, and surrounding whitespace before sending text to remote MT/LLM providers.
  - Validates placeholder count parity, residual mask tokens, and trailing `\t` mismatches after restoration, flagging anomalies with `[⚠]`.

### 2. Cross-Platform Build & CLI Flags

#### Build Commands (`CGO_ENABLED=0`)

```bash
# macOS (Apple Silicon / Intel)
CGO_ENABLED=0 go build -ldflags="-s -w" -o locloom .

# Windows (GUI executable without console window)
CGO_ENABLED=0 go build -ldflags="-s -w -H=windowsgui" -o locloom.exe .

# Linux
CGO_ENABLED=0 go build -ldflags="-s -w" -o locloom .
```

#### CLI Flags

| Flag | Description | Example |
| :--- | :--- | :--- |
| `-src <path>` | Initial localization directory or `.zip` archive to load on startup | `-src ./i18n/ts` |
| `-ts <path>` | Alias for `-src`: initial `.ts` directory or `.zip` path | `-ts ./i18n/ts` |
| `-xlsx <path>` | Initial `.xlsx` glossary/translation workbook to import on startup | `-xlsx ./glossary.xlsx` |
| `-theme <mode>` | UI theme override: `system` (default), `light`, `dark` | `-theme dark` |
| `-lang <mode>` | UI language override: `zh` (default), `en`, `auto` | `-lang en` |
| `-open-help` | Open the built-in usage guide modal on startup | `-open-help` |
| `-headless-render <png>` | Render a single UI frame to a PNG file and exit (smoke verification) | `-headless-render ./preview.png` |
| `-width <px>` | Initial window width (default `1280`) | `-width 1440` |
| `-height <px>` | Initial window height (default `820`) | `-height 900` |

```bash
# macOS / Linux example
./locloom -src ./translations -xlsx ./translations.xlsx -theme dark

# Windows PowerShell example
.\locloom.exe -src .\translations -xlsx .\translations.xlsx -theme system
```

### 3. Standard Workflow & Status Badges

1. **Load Target Directory or `.zip`**: Click **Browse Dir…** / **Browse ZIP…** in the header (or drag and drop a folder / `.zip` onto the window). Subdirectories, relative paths, custom filename patterns (e.g., `{id}.ts`), and unknown locales are discovered automatically.
2. **Import / Export `.xlsx`**: Drag and drop an `.xlsx` file or configure it in the sidebar. Supports `Context`, `Source`, `Comment`, and multi-locale columns (reference columns labeled `ref` / `参考` are skipped). Shared read mode on Windows allows reading workbooks even while open in Excel. Export modes include **Gaps Only**, **Full Matrix**, and **Current Changes**.
3. **Configure Waterfall & Proxy**: Enable, reorder, and test providers (`XLSX`, `TS TM`, `Google Web`, `OpenAI / Ollama`) and optionally configure HTTP/SOCKS5 proxies.
4. **Review & Write Back Safely**: Switch between **Focus Locale** and **Full Matrix**, inspect line-by-line XML diffs in the bottom inspector, and write back with optional `.bak` backups (directory mode) or `*_updated.zip` output preserving all non-`.ts` entries verbatim (ZIP mode).
5. **Real-Time UI Language Switching**: Click the **`[EN]` / `[中文]`** button in the top-right header to toggle the entire workbench (menus, provider schemas, validation warnings, and built-in help) between English and Simplified Chinese on the fly, or pass `-lang <auto|zh|en>` on startup.

Status badges are strictly for UI preview and are **never** written to `.ts` or `.xlsx` files:

| Badge | Meaning | Behavior |
| :--- | :--- | :--- |
| **`[补]`** | Fill Gap | Original entry is `unfinished` or empty; writes translation and removes `type="unfinished"` |
| **`[改]`** | Overwrite | Shown only in Overwrite mode; replaces an existing finished translation |
| **`[新]` / `[加]`** | Append Missing | Shown when Append Missing is enabled; inserts a new entry into the target `<context>` |
| **`[⚠]`** | Format Warning | Placeholder count (`%1` / `%n`), mask token, or trailing `\t` differs from the source text |

### 4. OS Configuration & Cache Paths

LocLoom follows standard OS application directories and supports **Portable Mode**:

| Platform | Configuration File (`config.json`) | Cache File (`translation-cache.json`) |
| :--- | :--- | :--- |
| **macOS** | `~/Library/Application Support/locloom/config.json` | `~/Library/Caches/locloom/translation-cache.json` |
| **Windows** | `%AppData%\locloom\config.json` | `%LocalAppData%\locloom\translation-cache.json` |
| **Linux** | `~/.config/locloom/config.json` | `~/.cache/locloom/translation-cache.json` |
| **Portable Mode** | If `config.json` exists alongside the executable, LocLoom reads and writes it there |

### 5. Multi-Format Architecture Note

LocLoom's internal domain model (`internal/domain` and `internal/planner`) is format-agnostic and centered on `(Context, Source, Comment)` keys, locale catalogs, and surgical byte-range patching. While Qt `.ts` is the first-class format implemented today, the architecture is designed to expand cleanly to additional localization ecosystems, including **GNU gettext (`.po`)**, **Apple String Catalogs (`.xcstrings`)**, **XLIFF (`.xliff` / `.xlf`)**, **Flutter/Web (`.arb` / JSON)**, **Android (`strings.xml`)**, and **.NET (`.resx`)**.

---

<a id="简体中文"></a>

## 简体中文

**LocLoom** 是一款基于 Go 与 [`egoist/mygo`](https://github.com/egoist/mygo) 构建的跨平台单文件桌面与命令行本地化工作台（`CGO_ENABLED=0`），支持 **macOS**、**Windows** 与 **Linux**。工具围绕“原位无损回填”设计，支持提取多语言词条（当前首发支持 Qt Linguist `.ts` 目录与 `.zip` 压缩包）、通过四级并发瀑布流自动补齐译文、导出多模式 `.xlsx` 审校表，并以**原位字节偏移补丁**写回目标文件。

### 1. 核心架构与能力

- **原位字节偏移无损写回（Surgical Byte-Offset XML Patching）**
  - 不使用全量 XML 重序列化，而是精确计算每个 `<message>` 内 `<translation>` 节点的 UTF-8 字节偏移进行局部替换。
  - 严格保留原文件的 **UTF-8 BOM**、**CRLF / LF 换行符**、缩进、XML 注释、`<!DOCTYPE TS>`、`<location>` 节点、`<byte value="x..."/>` 控制字符以及 `<numerusform>` 复数形式。
  - 仅按 Qt `lupdate` 规范转义 `&`、`<`、`>`，避免实体二次转义，写盘前在内存中通过 `encoding/xml` 完整校验语法合法性。
- **上下文安全去重与幽灵词条隔离**
  - 以 `(Context, Source, Comment)` 三元组（`EntryKey`）唯一标识词条；不同 `<context>` 下相同 `<source>`（如 `FileMenu` 与 `ConnectionState` 中的 `Open`）互不串扰。
  - 默认严格隔离仅存在于 `.xlsx` 中的外部词条，防止向 `.ts` 注入“幽灵词条”；仅在显式开启 **「追加缺失」** 时才写入目标 `<context>`。
- **四级并发瀑布流引擎（4-Stage Concurrent Waterfall）**
  - 按优先级顺序链式回退补缺：**本地 `.xlsx` 词条表** → **本地 `.ts` 翻译记忆库 (TM)** → **Google Web（免 Key 批量）** → **OpenAI / Ollama 兼容大模型**。
  - 支持多语种并发调度、持久化本地缓存（`translation-cache.json`）以及 **HTTP / SOCKS5 代理**（支持自签证书网关跳过 TLS 校验）。
- **Qt 占位符与格式护栏**
  - 发送给远端机器翻译 / 大模型前自动掩码 `%1`..`%99`、`%L1`..`%L99`、`%n` 与前后空白；还原后自动校验占位符数量一致性、残留掩码及末尾制表符 `\t` 差异并标记 `[⚠]`。

### 2. 跨平台编译与命令行参数

#### 编译命令（全平台 `CGO_ENABLED=0`）

```bash
# macOS (Apple Silicon / Intel)
CGO_ENABLED=0 go build -ldflags="-s -w" -o locloom .

# Windows (隐藏控制台窗口的 GUI 可执行文件)
CGO_ENABLED=0 go build -ldflags="-s -w -H=windowsgui" -o locloom.exe .

# Linux
CGO_ENABLED=0 go build -ldflags="-s -w" -o locloom .
```

#### 命令行启动参数

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

### 3. 标准工作流与状态标记

1. **加载目标目录或 `.zip`**：点击顶部工具栏 **「浏览目录…」** / **「浏览 ZIP…」**（或直接拖入窗口）。自动递归扫描子目录、保留相对路径、支持 `{id}.ts` 命名模板并自动发现未知语种。
2. **导入 / 导出 `.xlsx` 词条表**：拖入 `.xlsx` 或在左侧启用词条表，支持 `Context`、`Source`、`Comment` 及多语种列（表头含 `参考` / `ref` 的列默认跳过）。Windows 下采用共享读取模式，即使 Excel 正在打开文件也可直接读取。支持导出 **未翻译缺口**、**全量总表** 与 **本轮变更**。
3. **配置翻译源与代理**：在左侧勾选并排序翻译源（`XLSX`、`TS TM`、`Google Web`、`OpenAI / Ollama`），支持模型自动发现与 HTTP/SOCKS5 代理配置。
4. **审校与安全写回**：支持 **聚焦单语种** 与 **全景矩阵** 视图切换，底部检查栏可逐行预览 XML 片段差异；目录模式支持生成 `.bak` 备份并原子替换，ZIP 模式输出 `*_updated.zip` 并完整保留压缩包内的非 `.ts` 文件与目录结构。
5. **中英文实时切换**：点击顶部工具栏右上角 **`[EN]` / `[中文]`** 按钮，即可实时切换全界面、翻译引擎配置表单、占位符告警及内置帮助文档语言；也可通过 `-lang <auto|zh|en>` 启动参数指定。

单元格状态前缀仅用于界面提示，**绝不会写入 `.ts` 或 `.xlsx`**：

| 标记 | 含义 | 说明 |
| :--- | :--- | :--- |
| **`[补]`** | 补缺写入 | 原 `.ts` 为 `unfinished` 或空译文，写入时填入新译文并移除 `type="unfinished"` |
| **`[改]`** | 覆盖已有 | 仅在「覆盖已有」模式下出现，替换原 `.ts` 中已完成的旧译文 |
| **`[新]` / `[加]`** | 追加新词条 | 仅在勾选「追加缺失」时出现，将 `.ts` 中不存在的词条追加到目标 `<context>` |
| **`[⚠]`** | 格式告警 | `%1` / `%n` 占位符、掩码标记或末尾 `\t` 与原文不一致 |

### 4. 配置文件与缓存路径

LocLoom 自动遵循各操作系统的标准目录规范，并支持**便携模式（Portable Mode）**：

| 平台 | 配置文件路径 (`config.json`) | 缓存文件路径 (`translation-cache.json`) |
| :--- | :--- | :--- |
| **macOS** | `~/Library/Application Support/locloom/config.json` | `~/Library/Caches/locloom/translation-cache.json` |
| **Windows** | `%AppData%\locloom\config.json` | `%LocalAppData%\locloom\translation-cache.json` |
| **Linux** | `~/.config/locloom/config.json` | `~/.cache/locloom/translation-cache.json` |
| **便携模式** | 若可执行文件同级目录已存在 `config.json`，优先读写同级目录文件 |

### 5. 多格式扩展架构说明

LocLoom 的核心领域模型（`internal/domain` 与 `internal/planner`）采用与具体文件格式解耦的设计，围绕 `(Context, Source, Comment)` 三元组索引、多语种目录与原位字节补丁构建。当前首发完整支持 Qt `.ts`，该架构已为平滑扩展至 **GNU gettext (`.po`)**、**Apple String Catalogs (`.xcstrings`)**、**XLIFF (`.xliff` / `.xlf`)**、**Flutter / Web (`.arb` / JSON)**、**Android (`strings.xml`)** 及 **.NET (`.resx`)** 等主流本地化生态做好了抽象准备。
