# loong — 通用软件平台设计设想

> 状态：v0.1 设计稿（讨论中）
> 最后更新：2026-08-27

## 1. 定位与目标

**一句话**：loong 是一个"业务平台"。外部项目引入 loong 后，只需挂载自己的业务组件，登录 / 配置 / 日志 / 推送等通用能力开箱即用。

**痛点**：每次启动新软件项目都要从头搭建系统（登录、配置、日志、推送、用户体系……），重复劳动占掉大量时间。

**目标**：把"每次从零搭系统"变成"只写业务"。外部引入 loong 时，只需要关注本身的业务。

**边界**：

- 平台 = 内核 + 平台组件（渠道接入层 + 平台核心）
- 业务方 = 自己的业务组件
- 平台不预设业务，能力以组件形式插拔

## 2. 核心架构：组件树

整个软件体系是一棵**组件树**（component tree）。

- **节点 = 组件实例**：由「类型工厂 + 实例配置 + 生命周期」构成
- **根** = 应用装配点（Application Root）
- 树表达三种关系：
  - **包含（组装）**：父节点挂载子节点，决定树的形状
  - **作用域（配置继承）**：实例配置沿父链继承 + 覆盖
  - **通讯路径（父子）**：下行配置注入、上行事件上报

**技术选型**：

- **实现语言：Go**。
- **独立内核**：loong 自己实现内核，不直接复用 owl 代码。
- **思想继承 owl**：build / run / stop 三阶段生命周期、init() 自注册、type-based 服务 key 等已验证机制沿用，接口可自由调整。
- **单进程单体**：整棵组件树跑在一个进程内，组件间调用零开销，简单可调试。
- **配置树格式：YAML**，两段式解析（schema-per-component），详见 4.2。
- **日志：标准库 log/slog**，console（彩色文本，默认）/ json 双格式、级别可配，零外部依赖。
- **服务查找：Scope.Get[T]()** 树优先级（唯一提供者或父链最近），Go 类型即 key（不用字符串）。
- **Web 渠道：标准库 net/http（Go 1.22 方法+路径路由）**；会话令牌由独立 `auth` 组件提供（golang-jwt），渠道组件本身不持有任何鉴权/账号逻辑。
- **用户存储：modernc.org/sqlite（纯 Go，无 cgo）**，内核留存储接口，后续可换后端。
- **微信 API：自写最小 HTTP 封装**（code2session / OAuth / 模板消息 / 回调验签），不引入 SDK。
- **模块名：github.com/zsying/loong**。

### 2.1 已定决策清单

| 项 | 决策 |
|---|---|
| 实现语言 | Go |
| 内核 | 独立实现，思想继承 owl（build/run/stop 三阶段、init() 自注册、type-based 服务 key）；提供 Base 骨架组件 |
| 部署形态 | 单进程单体 |
| 配置树 | YAML，两段式解析（schema-per-component），支持 `${ENV}` 展开 |
| 组件 config | `WithConfig[T]` 声明结构（`Components()` 展示字段）；`scope.Config[T]()` 严格解码（未知字段报错） |
| 事件协议 | `{Name, Source, Payload}`，直达直接父节点，`On(name, handler)` 订阅 |
| 服务查找 | `Scope.Get[T]()` 树优先级（唯一提供者或父链最近；歧义提示 `GetFrom[T](id)`），Go 类型即 key |
| 服务组件 | 注册选项 `WithService[T](get)`（可多个）；激活规则与普通组件一致（默认装配激活 / yaml `lazy: true` 按需），多实例并存 |
| 组件上下文 | `loong.Scope`（刻意避开标准库 `context.Context` 同名冲突） |
| 日志 | 标准库 log/slog，console（彩色）/ json 双格式、级别可配 |
| 用户存储 | modernc.org/sqlite（纯 Go，无 cgo） |
| Web 渠道 | 标准库 net/http 通道（`*web.Router` 注册面）；会话 `auth` 组件（golang-jwt） |
| 微信 API | 自写最小 HTTP 封装（不引入 SDK） |
| 邮件 / unionid | 扩展组件，v0.1 不实现 |
| 仓库结构 | 核心在根 loong 包 + components/（含 cli）+ examples/（含 cli）+ docs/ |
| v0.1 验收渠道 | Web / CLI |

### 2.2 仓库结构

```
loong/
├── *.go           # 内核（package loong，仓库根即平台核心）
├── components/    # 平台组件：log / user / auth / web（web/static、web/account 可选）/ cli（cli/commands 为可选命令集）
├── examples/      # 示例项目（hello：Web 渠道；cli：CLI 组件化模板）
└── docs/          # 设计文档
```

## 3. 平台能力分层

```
Application Root
├── 渠道接入层 Channels（协议翻译：外部请求 → 平台内部调用）
│   ├── Web 平台
│   ├── TUI
│   ├── 微信小程序
│   └── 微信公众号
├── 平台核心 Platform Core（共享能力）
│   ├── 配置系统
│   ├── 日志系统
│   └── 用户体系
└── 业务层 Business（外部引入时唯一需要写的一层）
    ├── 业务组件 A
    ├── 业务组件 B
    └── …按需挂载
```

分层的意义：渠道层负责"长得不一样"（协议 / UI / 适配器），核心负责"能力一样"，业务只关心自己的事。三层演化速度不同，改动互不影响。

**核心组件 vs 扩展组件**：核心组件由平台预装、开箱即用；扩展组件不预装，按项目需要以组件形式引入（邮件服务、微信 unionid 打通等）。这套边界保证平台内核保持最小，能力按需生长。

## 4. 关键设计点

### 4.1 父子节点通讯（双通道 + 旁路）

- **下行（装配期）**：父 → 子，配置注入（props）。父决定子怎么配置，声明式，子只读。
- **上行（运行期）**：子 → 父，事件上报（event）。子不持有父的引用，只发事件，父决定怎么处理。
- **旁路**：服务查找。子组件按"类型 key"从树中取平台服务（配置 / 日志 / 用户…），不必绕父节点。
- **约束**：兄弟节点不直连，需要协作就走共同父节点或服务。保证树形结构不被破坏。

**事件协议（v0.1）**：

- **事件结构**：

  ```go
  type Event struct {
      Name    string // 事件名，<域>.<动作>，如 user.login / biz.order.created
      Source  string // 来源节点 id（组件实例 id）
      Payload any    // 负载，组件自描述类型
  }
  ```

- **命名规则**：`<域>.<动作>`，小写点分。域用业务语义（`biz.*`、`user.*`、`wechat.*`），不与具体组件类型强绑，业务组件可自由发事件。
- **路由**：事件默认只投递给**直接父节点**；父节点通过 `On(name, handler)` 注册 handler，未注册的事件静默丢弃。子组件发事件时不知道父是谁——不持有父引用，内核负责按树投递。
- **同步执行**：handler 在内核调度内同步调用，必须快速返回；长任务由组件自行异步化（`go` 或任务队列扩展组件）。渠道回调（如微信消息）由渠道组件内部异步化后再 Emit。
- **负载自描述**：`Payload` 是不透明值，父组件自行 type-assert——与配置树 schema-per-component 同一哲学：平台零改动即可支持新事件。
- **订阅端类型安全**：可用泛型方法 `Scope.OnTyped[T](name, handler)` 把 payload 断言为 T（编译期写死期望类型，断言失败返回错误）。事件按字符串名动态分发，路由表本身无法参数化，故泛型封装在订阅端方法（Go 1.27+ 泛型方法），内核 `Payload` 保持不透明。
- **不做全局事件总线**：兄弟协作仍走共同父节点或服务查找（见上约束）；全局通知需求由根节点组件自行实现。

### 4.2 组件组装（静态声明 + 动态注册）

- **配置树**声明结构：谁挂在哪、父给什么配置。
- **init() 自注册**声明类型：组件类型自己向内核注册；注册选项声明服务（`WithService[T](get)`，可多个）、配置结构（`WithConfig[T]`）、发出的事件（`WithEvents`）与描述（`WithDesc`）。
- **装配器**流程（登记 / 激活两阶段）：读配置树 → 登记（类型校验、id 填充与查重、建索引——零实例化）→ 激活必需节点（非 lazy 节点按 build / run 两阶段启动，先父后子）→ 运行期按需激活 yaml `lazy: true` 节点；Shutdown 对已激活节点按 Run 的逆序调 Stop，未激活节点跳过。
- **一键启动**：`loong.LoadAndRun(path, loong.WithWait())` 合并“读配置树 → 登记 → 激活 → 运行”，`WithWait` 时阻塞到 SIGINT/SIGTERM 再优雅关闭；底层 `LoadTree` / `New` / `Assemble` 仍可直接调用（测试、嵌入式场景）。
- **Base 骨架**：loong 包提供可嵌入的 Base（空默认三方法 + Scope + Emit / Logger），组件嵌入后只需覆盖关心的阶段——核心接口保持最小，复杂度按需覆盖。
- 运行期支持动态启用 / 禁用 / 替换实例。

**生命周期阶段**（装配一次、运行长期；对应 React 的 mount / unmount，无 update 循环——配置树装配后不再变化）：

| 阶段 | 时机 | 职责 |
|---|---|---|
| Build | 激活节点后、Run 前（先父后子） | 解码 config、Get 依赖服务（可能触发惰性激活）、订阅子节点事件、接线 |
| Run | Build 成功后（先父后子） | 启动服务（先接线后点火） |
| Stop | Shutdown 时按 Run 逆序（子先父后） | 优雅关闭（关 server / db / flush） |

**惰性与按需激活**：激活只有一条规则——**默认装配激活，yaml `lazy: true` 显式跳过**，组件是否提供服务不改变它。登记阶段只校验与建索引，不实例化；lazy 节点在首次 `Get[T]()`（service）或 `Scope.Activate` / `Kernel.Activate(id)` 时被激活。装配期激活保持"先全树 Build 再全树 Run"；lazy 路径是"实例化 + Build + Run 一体"，依赖 DAG 由 Build 期的 Get 推导（父组件 Build 时 Get 懒服务 → 先激活服务再继续），天然"先依赖后依赖方"，同时消除了对配置树声明顺序的依赖。激活由内核互斥保证单例幂等。

**配置树设计（YAML · 两段式解析）**：

- 节点 schema 统一：`{type, id?, config?, children?}`，平台只解析这 4 个字段；`config` 是不透明字节（yaml.Node），原样传给对应组件，不再深入。
- **根节点就是配置树的第一个节点**（无需 `root:` 包装键）；根通常是内核内置的 **`base` 容器**（无行为生命周期，`loong.Container` 注册），项目无需为纯容器根声明自定义组件。
- **组件自描述（schema-per-component）**：组件注册自己的配置 struct（`WithConfig[T]`，元数据进 `Components()`：字段名 / 类型 / 可选性），Build 里用 `scope.Config[T]()` 解码——**严格模式**：未知字段报错并带节点 id（typo 在激活期暴露而非静默忽略）；无 config 块返回零值。使用者在写 yaml 前即可通过 `Components()` 查看组件接受哪些 key。
- 平台永远不需要知道完整 schema → 新增组件只需注册自己的 struct，配置结构随组件自由变化，无需改平台。
- 继承与覆盖优先级：实例 config（配置树声明）> 父组件 build 期增强（下行 props）> 组件默认值。
- 环境差异（dev / prod）：v0.1 用 `${ENV}` 环境变量覆盖，不做多层配置合并。注意展开是**文本级**（YAML 解析前替换），值含 YAML 敏感字符（`:` `#` 引号）时需在配置里加引号。

```yaml
# loong.yaml — 每个项目的配置树（根可用内核内置 base 容器，无需自定义）
type: base
config:
  name: myproject
children:
  - type: log
    id: logger
    config:
      level: info
  - type: user
    id: users
  - type: auth                # 会话令牌：跨渠道组件，只依赖 net/http
    id: sessions
    config:
      secret: ${JWT_SECRET}
  - type: web                 # web = 纯通道：listen + 注册面 *web.Router
    id: main
    config:
      listen: ":8080"
    children:
      - type: web.account     # 可选：标准账号 API（register/login/me），挂 web 下即用
        id: account
      - type: web.static      # 可选：静态托管 + SPA fallback（纯 API 后端不挂）
        id: site
        config:
          dir: ./dist
          spa: true
      - type: biz.books       # 业务组件挂到 web 下，Build 里 r.Get(...) 注册自己的端点
        id: books
        config:
          route: /api/books
  - type: wechat.miniprogram
    id: mp
    config:
      appid: wx123            # openid 登录 + 订阅消息
```

### 4.3 父节点决定组件要求（配置继承与覆盖）

核心原则：**组件类型 ≠ 组件实例**。

- 同一个组件类型可以挂多个实例，各自挂在不同父节点下。
- **多实例机制**：配置树里声明多个同 type 节点即可（id 唯一，缺省 id = type）；init() 注册的类型工厂每次调用返回**新实例**，各实例的 config / 事件 / 生命周期完全独立。
- 实例配置 = 父链默认值 + 父节点覆盖 + 实例自身声明。
- 例：用户体系在 Web 下表现为会话登录，在小程序下表现为 openid 登录；日志在 API 下输出 JSON、在 TUI 下输出 ANSI 彩色——组件本身不改，读父链下发的配置 / 角色决定行为。
- **服务提供的约定**：服务的**唯一声明入口**是注册选项 `WithService[T](get)`——`get` 是取值函数，激活后从组件实例取出服务值，类型由泛型参数编译期固定（无 `any`、无运行期校验）。一个组件可声明多个服务（多次 `WithService`）。服务按「类型 → 节点 id」登记，**同类型允许多个提供者并存**（如 main / admin 两个 web 实例），不再有装配期唯一性约束。查找只通过 `Scope`：`scope.Get[T]()` 在唯一提供者时直接命中；多提供者时沿「自身 + 父链向上」取最近的声明者（组件挂在哪就属于哪，契合重用组件的父子约定），父链无匹配则报错提示 `GetFrom[T](id)` 按节点 id 显式取。惰性激活遵循同一优先级：多候选时只激活父链命中的节点。**激活与"是否提供服务"无关**——服务组件与其他组件一样默认装配激活，需要按需就在配置树标 `lazy: true`（首次查找时激活）；`TryGet / TryGetFrom` 报告错误，`Get / GetFrom` 返回零值；`Activate(id)` 可手动激活任意 lazy 节点。组件发现用 `loong.Components()`（type / desc / service 类型 / emits / config 元数据）。
- **装配失败清理**：Build / Run 阶段任一组件失败，已 Build 的组件会按逆序 Stop（释放 db / server 等资源），`Shutdown` 在未装配或装配失败后调用均为安全空操作。
- **约束**：组件的可变部分必须走配置 / 接口，不能写死全局状态。

### 4.4 CLI 组件化（cli 模式）

CLI 应用与常驻渠道共用同一心智：**父组件定义其子节点的激活方式**（`Scope.Activate`），激活链全程是框架能力，无外部分发代码。`components/cli` 提供：

- **父定义子激活（框架原语）**：`Scope.Activate(id string, args any)` —— 父节点激活自己的直接子节点，参数经子节点的 `Scope.Args` 注入；激活保持按节点幂等（参数首次激活生效）。这是通用能力，不限于 CLI（向导流程、状态机、插件选择皆可用）。
- **cli 核心（`components/cli`）**：总控 `cli`（type `cli`，Run 解析 os.Args 激活首段命令并传参，无参数/未知命令返回 `ErrUsage`）+ 通用组容器 `cli.group`（把第一参数当子命令 `Activate` 下去，零定制，多级命令即树层级）+ 参数辅助 `HasFlag` / `Flag` / `Positional`：flag 拼写按惯例绑定——单字母短 flag 用 `-x`、单词长 flag 用 `--word`（`-html`/`--h` 这类混搭不命中）；同一选项要同时接受短长两种拼写就并列名字（`HasFlag(args, "h", "html")` 匹配 `-h` 或 `--html`，值 flag 同理 `Flag(args, "o", "output")` 匹配 `-o=out` 或 `--output=out`）。**只引入核心即可开发 CLI**。
- **平台命令（可选，`components/cli/commands`）**：`cli.list` / `cli.new` / `cli.tree` 独立子包，按需 `_ import`——不需要就不引入（yaml 里也不出现对应类型）。
- **命令 = lazy 子组件**：业务命令挂载在总控下，`Run` 里读 `ctx.Args` 执行；CLI 是完整 loong 应用，同时挂载 `log` 组件获得日志（命令/错误经 slog 记录）。
- **退出码**：`ErrUsage` sentinel 标记用法错误（错误分类由 cli 包给出，映射到具体数字是应用级决策——examples/cli 用 2，且**任何错误先记录再退出**）。命令在装配期执行（总控 Run 触发），`main` 只负责启动 + 错误映射 + `Shutdown`。
- **入口 = 普通 loong 应用**：不提供启动封装（无 cli.Go）——配置树内联用 `Parse + New + Assemble`，文件用 `loong.LoadAndRun`，退出码映射由 main 决定；examples/cli 展示两种入口。
- 任何 loong 应用挂载这些组件即可获得组件化 CLI；`examples/cli` 是完整模板（总控 + log + 业务命令 + 多级命令 + 平台命令）。扩展命令 = 注册组件类型 + yaml 加 lazy 节点。

### 4.5 Web 通道组件化（web 模式）

web 组件定位为**纯 HTTP 通道**：server 生命周期（listen / 优雅关停 / 超时）+ 一个注册面服务 `*web.Router`。会话、账号 API、静态托管、业务端点一律是挂到 web 下的组件——配置树声明即装配，与 cli 家族（总控 + 可选命令）同一心智：

- **`web`（`components/web`）**：Config 只含 `listen`（`log: false` 可关请求日志）。**不 import user/auth、不订阅业务事件**；默认中间件 Recover（panic → 500）+ 请求日志（Debug）。
- **注册面 `*web.Router`（服务）**：`Handle` / `Get` / `Post` / `Put` / `Patch` / `Delete`、`Group(prefix, mws...)`（前缀 + 组中间件，如 `/admin` + Guard）、`Use`（全局中间件）、`ServeHTTP`（可直接测试）。底层是 stdlib ServeMux（Go 1.22 方法+路径），中间件类型 = `func(http.Handler) http.Handler`，net/http 生态全兼容；附带 `WriteJSON` / `ReadJSON` 助手。业务组件注册端点不再接触 mux。规则：root 的 `Use` 中间件是 server 级（ServeHTTP 统一应用一次），Group 只继承父 group 的注册级中间件；同一 method+path 重复注册 = 装配期 panic（视为路由冲突错误）。
- **`auth`（`components/auth`，跨渠道可复用）**：Config{secret, ttl}；服务 `Issue(sub)` / `Guard(next)` / 包级 `Identity(r)`；只依赖 net/http + golang-jwt，不认识 web/user。
- **`web.account`（可选）**：标准账号 API register / login / me，编排 `user.Service` + `auth.Service`，挂到 web 下即用；不需要标准密码登录就不挂。
- **`web.static`（可选）**：Config{dir, prefix?="/", spa, api?}，向父 Router 注册前缀挂载静态树（spa 时未命中文件回退 index.html；`api: /api` 前缀的未命中路径保持 404，SPA fallback 不遮蔽 API 路由）。

装配示例（API 后端 + 静态站 + 账号）：

```yaml
children:
  - type: user
    id: users
  - type: auth
    id: sessions
    config:
      secret: ${JWT_SECRET}
  - type: web
    id: main
    config:
      listen: ":8080"
    children:
      - type: web.account      # 账号 API（可选）
      - type: web.static       # 静态站（可选），纯 API 后端不挂
        config: { dir: ./dist, spa: true }
      - type: biz.books        # 业务组件：Build 里 ctx.Get[*web.Router]() 注册端点
```

鉴权 = 显式选择（与 CLI flag「显式声明别名」同哲学）：业务组件对敏感路由用 `Group("/admin", authSvc.Guard)` 或 `r.Handle("GET", "/x", authSvc.Guard(h))`，不做隐形全局规则。user/auth 与 web 的树位置无硬性要求——服务查找按需激活、唯一提供者全局命中（多实例时沿父链就近或用 `GetFrom[T](id)`），默认把能力组件与使用它们的通道放同一子树更清晰。

设计动机：旧实现把会话 / 账号端点 / 静态托管 / 裸 mux 注册 / 业务事件订阅全塞进一个 web 组件（纯为 examples/hello 演示），换鉴权方案要改通道源码、平台组件还认识业务事件。拆分后 web 保持最小，业务扩展 = 挂子组件 + 写注册代码，零新概念。

## 5. 渠道适配

### 5.1 网站平台

- HTTP 路由、静态资源托管、鉴权中间件。
- 前端可用任意技术（React / Vue / 原生），由平台托管静态产物。
- 与用户体系联动：会话登录（session / token）。

### 5.2 微信小程序

- **openid 登录**：wx.login → code 换 openid → 平台用户体系建档 / 绑定。
- **消息推送**：订阅消息授权（wx.requestSubscribeMessage）→ 服务端按模板下发。

### 5.3 微信公众号

- **网页授权登录**：公众号菜单 / 文章内 H5，OAuth 静默或手动授权取 openid。
- **模板 / 订阅消息**：向关注用户推送通知。
- **消息收发**：用户发消息进入平台组件树的事件流。
- **关注 / 取关事件**：接收关注、取关、扫码等事件回调。

### 5.4 TUI

- 终端界面：彩色日志、交互式命令面板。
- 适合开发调试、服务端运维场景。

## 6. 组件编写规范（草案）

- 可变部分走配置 / 接口，不写死全局状态。
- 上行靠事件，下行靠配置，协作靠服务查找。
- **优先嵌入 `loong.Base`**，只覆盖关心的生命周期阶段。
- 遵守 build / run / stop 三阶段生命周期；Stop 里释放自己持有的资源（server / db / flush）。
- 不直接持有父节点 / 兄弟节点引用。
- **服务缺失即报错，不静默**：`Get` 无提供者 / 歧义时返回零值，组件应显式校验并返回错误（web.account / web.static / hello greet 先例），避免"挂错父节点、能力没注册但进程照跑"的排查黑洞。
- **业务扩展 = 项目组件**：业务能力的扩展同样以项目组件形式实现（业务项目里定义组件类型 + init() 注册 + 配置树挂载），与平台组件共用同一套机制——平台不预设业务，能力以组件插拔。

## 7. v0.1 范围

**纳入**：

- 内核：组件树、装配器、三阶段生命周期、服务查找。
- 平台核心：配置系统、日志系统、用户体系。
- 至少一个渠道跑通：**Web**（v0.1 验收样板）。
- 组件编写规范落地为文档 + 示例组件。

**扩展组件**（不预装，按项目需要以组件形式引入）：邮件服务、微信 unionid 打通、数据库抽象、缓存、任务调度、支付、文件存储。

## 8. 后续规划（v0.2+）

- 微信小程序 / 公众号渠道完整实现（仓库结构中已预留位置）。
- TUI 渠道实现。
- 扩展组件落地：邮件服务、微信 unionid 打通、任务队列、缓存。
- 用户体系增强：unionid 字段预留、第三方账号绑定。
