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
- **日志：标准库 loong.log/slog**，console（彩色文本，默认）/ json 双格式、级别可配，零外部依赖。
- **服务查找：Scope.Get[T]()** 树优先级（唯一提供者或父链最近），Go 类型即 key（不用字符串）。
- **能力贡献：** 组件类型用 `WithContributes[Kind]()` 声明「本类型节点的 id 是 Kind 家族里的一个名字」，运行期才知道的名字用 `Scope.Provide[Kind](name)` 补；`Kernel.Contributions[Kind]()` 枚举，结构性的先于运行期的。与服务的区别：贡献只有名字，不要实例，所以从 skeleton 就能回答。
- **Web 渠道：标准库 net/http（Go 1.22 方法+路径路由）**；会话令牌由独立 `loong.auth` 组件提供（golang-jwt），渠道组件本身不持有任何鉴权/账号逻辑。
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
| 服务查找 | `Scope.Get[T]()` 树优先级（唯一提供者或父链最近；歧义提示 `GetFrom[T](id)`），Go 类型即 key；`Kernel.Providers[T]()` 枚举候选节点 id |
| 服务组件 | 注册选项 `WithService[T](get)`（可多个）；激活规则与普通组件一致（默认装配激活 / yaml `lazy: true` 按需激活整棵子树），多实例并存 |
| 可选服务 | `WithOptionalService[T](get)`：取值返回 nil 即「本节点不提供该服务」，该节点不再是提供者（查找跳过），装配不报错；消费方用 `TryGet` 把"没有"当作一种结果 |
| 能力贡献 | `WithContributes[Kind]()`（名字 = 节点 id，纯结构、未激活即可枚举）+ `Scope.Provide[Kind](name)`（运行期才知道的名字）；`Kernel.Contributions[Kind]()` 列出两者，一个名字只能有一个主 |
| 依赖观测 | `Kernel.Consumers[T]()`：实际取过 T 的节点 id（只在成功解析时记录，是 trace 不是声明） |
| 组件上下文 | `loong.Scope`（刻意避开标准库 `context.Context` 同名冲突）；`Scope.Args` 是父节点下发的激活参数（`any`，启动/服务激活为 nil） |
| CLI 参数 | 总控激活的每个节点只有一个载荷类型 `cli.Args`（`cli.ArgsOf(scope)` 取出）：peel 掉的全局 flag + 剩余 argv，取值入口 `Bool` / `String` / `Positional` / `Arg` / `Unused`；不再有「pre 收 `*Globals`、命令收 `[]string`」的双契约 |
| 日志 | 标准库 loong.log/slog，console（彩色）/ json 双格式、级别与输出流（stdout/stderr）可配；`loong.log` 组件是进程默认 logger 的唯一属主，对外发布 `*loong.log.Log`（`SetLevel` / `SetWriter` 旋钮），应用只拧旋钮、不再装第二个默认 |
| 用户存储 | modernc.org/sqlite（纯 Go，无 cgo） |
| Web 渠道 | 标准库 net/http 通道（`*web.Router` 注册面）；会话 `loong.auth` 组件（golang-jwt） |
| 微信 API | 自写最小 HTTP 封装（不引入 SDK） |
| 邮件 / unionid | 扩展组件，v0.1 不实现 |
| 仓库结构 | 核心在根 loong 包 + components/（含 loong.cli）+ examples/（含 loong.cli）+ docs/ |
| v0.1 验收渠道 | Web / CLI |

### 2.2 仓库结构

```
loong/
├── *.go           # 内核（package loong，仓库根即平台核心）
├── components/    # 平台组件：loong.log / loong.user / loong.auth / loong.web（loong.web/static、loong.web/account 可选）/ loong.cli（loong.cli/commands 为可选命令集）
├── examples/      # 示例项目（hello：Web 渠道；loong.cli：CLI 组件化模板）
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
      Name    string // 事件名，<域>.<动作>，如 loong.user.login / biz.order.created
      Source  string // 来源节点 id（组件实例 id）
      Payload any    // 负载，组件自描述类型
  }
  ```

- **命名规则**：`<域>.<动作>`，小写点分。域用业务语义（`biz.*`、`loong.user.*`、`wechat.*`），不与具体组件类型强绑，业务组件可自由发事件。
- **路由**：事件默认只投递给**直接父节点**；父节点通过 `On(name, handler)` 注册 handler，未注册的事件静默丢弃。子组件发事件时不知道父是谁——不持有父引用，内核负责按树投递。
- **同步执行**：handler 在内核调度内同步调用，必须快速返回；长任务由组件自行异步化（`go` 或任务队列扩展组件）。渠道回调（如微信消息）由渠道组件内部异步化后再 Emit。
- **负载自描述**：`Payload` 是不透明值，父组件自行 type-assert——与配置树 schema-per-component 同一哲学：平台零改动即可支持新事件。
- **订阅端类型安全**：可用泛型方法 `Scope.OnTyped[T](name, handler)` 把 payload 断言为 T（编译期写死期望类型，断言失败返回错误）。事件按字符串名动态分发，路由表本身无法参数化，故泛型封装在订阅端方法（Go 1.27+ 泛型方法），内核 `Payload` 保持不透明。
- **不做全局事件总线**：兄弟协作仍走共同父节点或服务查找（见上约束）；全局通知需求由根节点组件自行实现。

### 4.2 组件组装（静态声明 + 动态注册）

- **配置树**声明结构：谁挂在哪、父给什么配置。
- **init() 自注册**声明类型：组件类型自己向内核注册；注册选项声明服务（`WithService[T](get)`，可多个）、配置结构（`WithConfig[T]`）、发出的事件（`WithEvents`）与描述（`WithDesc`）。
- **装配器**流程（登记 / 激活两阶段）：读配置树 → 登记（类型校验、id 填充与查重、建索引——零实例化）→ 激活根节点（即整棵非 lazy 树按 build / run 两阶段启动，先父后子）；运行期按需激活 yaml `lazy: true` 节点（连同其非 lazy 子树）。`Assemble` 就是"根节点的一次激活"，没有第二套构建流程——惰性节点与随树启动的节点走同一条路径、同一套状态记录。Shutdown 对已激活节点按 Run 的逆序调 Stop，未激活节点跳过。
- **一键启动**：`loong.LoadAndRun(path, loong.WithWait())` 合并“读配置树 → 登记 → 激活 → 运行”，`WithWait` 时阻塞到 SIGINT/SIGTERM 再优雅关闭；底层 `LoadTree` / `New` / `Assemble` 仍可直接调用（测试、嵌入式场景）。
- **Base 骨架**：loong 包提供可嵌入的 Base（空默认三方法 + Scope + Emit / Logger），组件嵌入后只需覆盖关心的阶段——核心接口保持最小，复杂度按需覆盖。
- 运行期支持动态启用 / 禁用 / 替换实例。

**生命周期阶段**（装配一次、运行长期；对应 React 的 mount / unmount，无 update 循环——配置树装配后不再变化）：

| 阶段 | 时机 | 职责 |
|---|---|---|
| Build | 激活节点后、Run 前（先父后子） | 解码 config、Get 依赖服务（可能触发惰性激活）、订阅子节点事件、接线 |
| Run | Build 成功后（先父后子） | 启动服务（先接线后点火） |
| Stop | Shutdown 时按 Run 逆序（子先父后） | 优雅关闭（关 server / db / flush） |

**惰性与按需激活**：激活只有一条规则，而它作用在**子树**上——**默认装配激活，yaml `lazy: true` 让该节点及其下整棵子树都不激活**；组件是否提供服务不改变它。登记阶段只校验与建索引，不实例化。装配激活保持"先全树 Build 再全树 Run"；按需激活同样两阶段，只是范围是该节点锚定的子树：**先整棵子树 Build（父先子后），再整棵子树 Run（父先子后）**——子节点常依赖父节点 Build 出来的东西（路由挂在 router 上、监听在 server 上），"全 Build 完再统一 Run"正是这种接线的要求。锚点之下再标 `lazy` 的节点是它自己的决定，不在级联范围内，需要时单独激活。

触发按需激活的三种途径一致：`Scope.Activate(id, args)`（父节点定义子节点的激活，args 经 `Scope.Args` 只交给锚点，子树继承激活而不继承参数）、`Kernel.Activate(id)`、以及命中未激活 provider 的 `Get[T]()`。激活由内核互斥保证单例幂等：锚点缓存结果，级联中建起来的节点一并标记为已激活；子树起来一半失败时，已 Build 的部分按逆序 Stop 掉，失败信息点名子树里出错的那个节点。

**构建期循环**：既然激活带动整棵子树，Build 期间的服务查找就可能启动一棵子树，而那棵子树的 Build 又需要"正在 Build 的这个节点"提供的服务（owlet 即此形：`owlet.runtime` Build 时要 tools 的 registry，而 tools 下的 dispatch 子节点要 runtime 的 coordinator）。这不是内核多试几次能解开的顺序问题——两个 Build 都得先完成，而谁也完成不了。内核的做法是拒绝把正在 Build 的节点再建一次并点名它：否则该组件会被静默实例化两份（提供服务的那份还会撞上服务表）。解在树里：**provider 声明在 consumer 之上**，先构建的一方把服务备好，后者的 Build 直接取到（`Get[T]()` 命中已注册的 provider，不再触发激活）。

> 两点由此确定下来：① 读树而非跑树的视图（`Shutdown`、`Contributions[T]`、`NodeInfo`）走全树遍历，休眠子树照常列出——"树里有什么"不取决于有没有被激活；② 只想让某个子系统按需起来，就把 `lazy` 标在**子系统的根**上（owlet 的 `loong.web` + `owlet.dash` 即此形），父节点 `Activate` 一次即可整棵起来。

**配置树设计（YAML · 两段式解析）**：

- 节点 schema 统一：`{type, id?, desc?, lazy?, config?, children?}`；平台只解析这几个骨架字段，`config` 是不透明字节（yaml.Node），原样传给对应组件，不再深入；`desc` 供 CLI usage 等展示，`lazy` 表示以该节点为锚点的整棵子树默认不激活（激活时整棵按 build / run 两阶段起来）。
- **根节点就是配置树的第一个节点**（无需 `root:` 包装键）；根通常是内核内置的 **`loong.base` 容器**（无行为生命周期，`loong.Container` 注册），项目无需为纯容器根声明自定义组件。
- **组件自描述（schema-per-component）**：组件注册自己的配置 struct（`WithConfig[T]`，元数据进 `Components()`：字段名 / 类型 / 可选性），Build 里用 `scope.Config[T]()` 解码——**严格模式**：未知字段报错并带节点 id（typo 在激活期暴露而非静默忽略）；无 config 块返回零值。使用者在写 yaml 前即可通过 `Components()` 查看组件接受哪些 key。
- **实例视图（自省）**：`Kernel.Root()` / `Kernel.Node(id)` 返回 `NodeInfo{ID, Type, Desc, Lazy, ParentID, Children}`——装配后的只读结构视图，供 CLI usage、`tree` 子命令、管理页渲染；`Desc` 只回节点自己的声明（类型级回退是读取方的 `Describe(type)`），`ParentID` 够向上走。查询纯结构：不激活任何节点。
- 平台永远不需要知道完整 schema → 新增组件只需注册自己的 struct，配置结构随组件自由变化，无需改平台。
- 继承与覆盖优先级：实例 config（配置树声明）> 父组件 build 期增强（下行 props）> 组件默认值。
- 环境差异（dev / prod）：v0.1 用 `${ENV}` 环境变量覆盖，不做多层配置合并。注意展开是**文本级**（YAML 解析前替换），值含 YAML 敏感字符（`:` `#` 引号）时需在配置里加引号。

```yaml
# loong.yaml — 每个项目的配置树（根可用内核内置 loong.base 容器，无需自定义）
type: loong.base
config:
  name: myproject
children:
  - type: loong.log
    id: logger
    config:
      level: info
  - type: loong.user
    id: users
  - type: loong.auth                # 会话令牌：跨渠道组件，只依赖 net/http
    id: sessions
    config:
      secret: ${JWT_SECRET}
  - type: loong.web                 # loong.web = 纯通道：listen + 注册面 *web.Router
    id: main
    config:
      listen: ":8080"
    children:
      - type: loong.web.account     # 可选：标准账号 API（register/login/me），挂 loong.web 下即用
        id: account
      - type: loong.web.static      # 可选：静态托管 + SPA fallback（纯 API 后端不挂）
        id: site
        config:
          dir: ./dist
          spa: true
      - type: biz.books       # 业务组件挂到 loong.web 下，Build 里 r.Get(...) 注册自己的端点
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
- **服务提供的约定**：服务的**唯一声明入口**是注册选项 `WithService[T](get)`——`get` 是取值函数，激活后从组件实例取出服务值，类型由泛型参数编译期固定（无 `any`、无运行期校验）。一个组件可声明多个服务（多次 `WithService`）。服务按「类型 → 节点 id」登记，**同类型允许多个提供者并存**（如 main / admin 两个 loong.web 实例），不再有装配期唯一性约束。查找只通过 `Scope`：`scope.Get[T]()` 在唯一提供者时直接命中；多提供者时沿「自身 + 父链向上」取最近的声明者（组件挂在哪就属于哪，契合重用组件的父子约定），父链无匹配则报错提示 `GetFrom[T](id)` 按节点 id 显式取。**候选集合是树里所有「组件类型声明了该服务」的节点，与"是哪个组件类型声明的"无关**——同一服务类型被多个组件类型声明是合法的，查找只按节点判断，不存在"每类型取一个"的索引。**T 允许是接口类型，且这是「一个能力、多个实现类型」时的唯一问法**：声明按接口类型登记，`Providers[T]()` 于是回答"哪些组件类型实现了这个能力"，查找返回的是组件本身（`v.(T)`），调用其方法即到达实现；`Consumers[T]()` 的轨迹同样按接口记账。owlet 的"一个工具一个组件类型"正是这个用法（工具节点即接口实现）。`Kernel.Providers[T]()` 枚举这些候选节点的 id（即 `GetFrom[T](id)` 接受的 id），是纯结构查询：lazy 节点未激活也列出，查询本身不激活任何节点。惰性激活遵循同一优先级：多候选时只激活父链命中的节点。**激活与"是否提供服务"无关**——服务组件与其他组件一样默认装配激活，需要按需就在配置树标 `lazy: true`（首次查找时激活）；`TryGet / TryGetFrom` 报告错误，`Get / GetFrom` 返回零值；`Activate(id)` 可手动激活任意 lazy 节点。组件发现用 `loong.Components()`（type / desc / service 类型 / 贡献家族 / emits / config 元数据）。

  两处补完：

  - **可选服务**：`WithOptionalService[T](get)` 与 `WithService` 形状相同，差别只在 nil 的含义。必选服务取到 nil 是错误（组件过早返回），可选服务取到 nil 是答案——「本节点没有这种能力」，该节点记入 skip 表，之后的查找（`Providers`/`GetFrom`/按需激活都算）不再把它当提供者，于是"没配后端就没这个能力"不必再发明哨兵值。消费方用 `TryGet` 把"没有"当作一种结果。注意 lazy 节点在激活前无法预知，激活后才发现为空时该次查找仍报错，换个提供者用 `GetFrom[T](id)`。
  - **依赖观测**：`Kernel.Consumers[T]()` 列出"实际取过 T"的节点 id（树序）。它记录的是运行事实而非声明：没跑过的 lazy 节点、查失败的调用都不出现，所以它是排障用的 trace，目录用途仍然找 `Providers[T]()`。

- **能力贡献的约定**：`WithContributes[Kind]()` 让一个组件类型的节点把自己的 id 贡献进 `Kind` 家族（Kind 是 Go 类型，与 service key 同哲学，不会串味）；`Scope.Provide[Kind](name)` 补上运行期才知道的名字（如 MCP server 报回的工具名）。`Kernel.Contributions[Kind]()` 按「结构性（树序）→ 运行期（注册序）」列出全部名字，**纯结构**：声明了但还没激活的节点也在列，查询不激活任何东西——这正是它相对"拿 registry 当判定源"的价值：消费方解析一个子树提供了什么，不再依赖"提供者必须排在消费者之前"。一个名字只能有一个主：同一节点重申自己的名字是 no-op，别的节点抢占则就地报错（歧义与重复节点 id 是同一类错误，只有调用方分得清，因为是它选的名字）。
- **装配失败清理**：Build / Run 阶段任一组件失败，已 Build 的组件会按逆序 Stop（释放 db / server 等资源），`Shutdown` 在未装配或装配失败后调用均为安全空操作。
- **约束**：组件的可变部分必须走配置 / 接口，不能写死全局状态。

### 4.4 CLI 组件化（loong.cli 模式）

CLI 应用与常驻渠道共用同一心智：**父组件定义其子节点的激活方式**（`Scope.Activate`），激活链全程是框架能力，无外部分发代码。`components/cli` 提供：

- **父定义子激活（框架原语）**：`Scope.Activate(id string, args any)` —— 父节点激活自己的直接子节点，参数经子节点的 `Scope.Args` 注入；激活保持按节点幂等（参数首次激活生效）。这是通用能力，不限于 CLI（向导流程、状态机、插件选择皆可用）。
- **loong.cli 核心（`components/cli`）**：总控 `loong.cli`（type `loong.cli`，Run 解析 os.Args 激活首段命令并传参，无参数/未知命令返回 `ErrUsage`）+ 通用组容器 `loong.cli.group`（把第一参数当子命令 `Activate` 下去，零定制，多级命令即树层级）+ 参数载荷 `*cli.Args`：总控激活的每个节点（pre 与命令）拿到的都是同一个类型，用 `cli.ArgsOf(ctx)` 取出——nil（启动 / 服务激活 / `Kernel.Activate`）即「没有参数」，其他类型则报错并说出实际与期望的两种类型，替代过去 `args, _ := ctx.Args.([]string)` 把类型不符读成「没有参数」。取值入口：`Bool(names...)` 是开关（`-x` 真、`-x=false` 假，不再让调用方比字符串），`String(names...)` 同时接受 `=value` 与 POSIX 空格式（`-o=out` / `-o out` / `--output out`），`Positional()` / `Arg(i)` 跳过 flag 且 `--` 之后全算位置参数，`Unused()` 报告没人认领的 flag（拼错不再静默）。flag 拼写按惯例绑定——单字母短 flag 用 `-x`、单词长 flag 用 `--word`（`-html`/`--h` 这类混搭不命中）；同一选项要同时接受短长两种拼写就在调用点并列名字（`args.Bool("h", "html")` 匹配 `-h` 或 `--html`）。被总控 peel 掉的全局 flag 是同一份载荷的一部分：按任一已声明拼写即可读到，而「声明了但 argv 没给」仍是缺席——声明不等于给出。`Peel(argv, flags)` 供不经树的宿主自行构造同一份载荷。**只引入核心即可开发 CLI**。
- **平台命令（可选，`components/cli/commands`）**：`loong.cli.list` / `loong.cli.new` / `loong.cli.tree` 独立子包，按需 `_ import`——不需要就不引入（yaml 里也不出现对应类型）。
- **命令 = lazy 子组件**：业务命令挂载在总控下，`Run` 里用 `cli.ArgsOf(ctx)` 读参数执行；CLI 是完整 loong 应用，同时挂载 `loong.log` 组件获得日志（命令/错误经 slog 记录）。
- **退出码**：`ErrUsage` sentinel 标记用法错误（错误分类由 loong.cli 包给出，映射到具体数字是应用级决策——examples/cli 用 2，且**任何错误先记录再退出**）。命令在装配期执行（总控 Run 触发），`main` 只负责启动 + 错误映射 + `Shutdown`。
- **入口 = 普通 loong 应用**：不提供启动封装（无 loong.cli.Go）——配置树内联用 `Parse + New + Assemble`，文件用 `loong.LoadAndRun`，退出码映射由 main 决定；examples/cli 展示两种入口。
- 任何 loong 应用挂载这些组件即可获得组件化 CLI；`examples/cli` 是完整模板（总控 + loong.log + 业务命令 + 多级命令 + 平台命令）。扩展命令 = 注册组件类型 + yaml 加 lazy 节点。

### 4.5 Web 通道组件化（loong.web 模式）

loong.web 组件定位为**纯 HTTP 通道**：server 生命周期（listen / 优雅关停 / 超时）+ 一个注册面服务 `*web.Router`。会话、账号 API、静态托管、业务端点一律是挂到 loong.web 下的组件——配置树声明即装配，与 loong.cli 家族（总控 + 可选命令）同一心智：

- **`loong.web`（`components/web`）**：Config 只含 `listen` / `tls` / `loong.log`。**不 import loong.user/loong.auth、不订阅业务事件**；默认中间件 Recover（panic → 500）+ 请求日志（Debug）。RequestLog 注册在前、Recover 在内，所以日志能记到 Recover 转换后的最终状态码。
- **HTTPS 是可选的**：配 `tls: {cert, key}` 才走 HTTPS，不配就是普通 HTTP——反代后面或内网部署不必碰证书。证书在 Run 里**同步** `LoadX509KeyPair`（再交给 `ServeTLS(ln, "", "")`），坏路径 = 装配失败，不会留下一个"在监听但什么也不服务"的进程。
- **中间件（stdlib 签名，可选组合）**：`Recover`（panic → 500，重抛 `http.ErrAbortHandler`）、`RequestLog`（method / path / status / duration）、`CORS(cfg)`（origin 白名单、预检 204、credentials、`Vary: Origin`；**不默认启用**——谁能跨域调这个 API 是业务决策，由组件 `r.Use(loong.web.CORS(cfg))` 自选）。**响应 wrapper 必须透明**：记录状态的中间件用 `responseRecorder` 包裹 writer，它必须转发 `Flush` / `Hijack` 并提供 `Unwrap`，否则 SSE、分块下载与 WebSocket 升级会被静默掐断——只嵌入 `http.ResponseWriter` 会让这些可选接口从方法集里消失。
- **注册面 `*web.Router`（服务）**：`Handle` / `Get` / `Post` / `Put` / `Patch` / `Delete`、`Group(prefix, mws...)`（前缀 + 组中间件，如 `/admin` + Guard）、`Use`（全局中间件）、`ServeHTTP`（可直接测试）。底层是 stdlib ServeMux（Go 1.22 方法+路径），中间件类型 = `func(http.Handler) http.Handler`，net/http 生态全兼容；附带 `WriteJSON` / `ReadJSON` 助手。业务组件注册端点不再接触 mux。规则：root 的 `Use` 中间件是 server 级（ServeHTTP 统一应用一次），Group 只继承父 group 的注册级中间件；**注册本身不 panic**——同一 method+path 重复注册、或路径不以 `/` 开头，都记进 `Router.Err()`（root 与所有 group 共享一份登记），loong.web 在 Run 开头检查并返回，于是路由冲突变成装配错误而不是把进程带走的 mux panic。
- **`loong.auth`（`components/auth`，跨渠道可复用）**：Config{secret, ttl}；服务 `Issue(sub)` / `Guard(next)` / 包级 `Identity(r)`；只依赖 net/http + golang-jwt，不认识 loong.web/loong.user。
- **`loong.web.account`（可选）**：标准账号 API register / login / me，编排 `loong.user.Service` + `loong.auth.Service`，挂到 loong.web 下即用；不需要标准密码登录就不挂。
- **`loong.web.static`（可选）**：Config{dir, prefix?="/", spa, api?}，向父 Router 注册前缀挂载静态树。URL 带 prefix、磁盘上的文件不带，所以文件查找前统一 strip（`prefix` + `spa` 组合同样生效，不能只在非 spa 分支裁剪）；spa 时未命中文件回退 index.html；`api: /api` 前缀的未命中路径保持 404，SPA fallback 不遮蔽 API 路由。

装配示例（API 后端 + 静态站 + 账号）：

```yaml
children:
  - type: loong.user
    id: users
  - type: loong.auth
    id: sessions
    config:
      secret: ${JWT_SECRET}
  - type: loong.web
    id: main
    config:
      listen: ":8080"
    children:
      - type: loong.web.account      # 账号 API（可选）
      - type: loong.web.static       # 静态站（可选），纯 API 后端不挂
        config: { dir: ./dist, spa: true }
      - type: biz.books        # 业务组件：Build 里 ctx.Get[*web.Router]() 注册端点
```

鉴权 = 显式选择（与 CLI flag「显式声明别名」同哲学）：业务组件对敏感路由用 `Group("/admin", authSvc.Guard)` 或 `r.Handle("GET", "/x", authSvc.Guard(h))`，不做隐形全局规则。loong.user/loong.auth 与 loong.web 的树位置无硬性要求——服务查找按需激活、唯一提供者全局命中（多实例时沿父链就近或用 `GetFrom[T](id)`），默认把能力组件与使用它们的通道放同一子树更清晰。

设计动机：旧实现把会话 / 账号端点 / 静态托管 / 裸 mux 注册 / 业务事件订阅全塞进一个 loong.web 组件（纯为 examples/hello 演示），换鉴权方案要改通道源码、平台组件还认识业务事件。拆分后 loong.web 保持最小，业务扩展 = 挂子组件 + 写注册代码，零新概念。

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
- **服务缺失即报错，不静默**：`Get` 无提供者 / 歧义时返回零值，组件应显式校验并返回错误（loong.web.account / loong.web.static / hello greet 先例），避免"挂错父节点、能力没注册但进程照跑"的排查黑洞。
- **Scope 与 context.Context 的边界**：`Scope` 服务生命周期与装配期（服务查找、子节点激活、本节点 config），它的有效期 = 节点生命周期；`context.Context` 负责 I/O 取消与超时（网络请求、子进程、外部调用）。规则：
  - 组件间协作（找服务、取配置、发事件）只走 `Scope`，不要把 `context.Context` 当依赖容器用（SetValues 传服务是反模式）。
  - `Run` 里启动的 goroutine 若做 I/O，自建 `context.WithCancel(context.Background())` 并把 cancel 存实例字段，`Stop` 里调用——组件不做跨阶段的 context 透传（树的生命周期由 Kernel 管理，不寄生在某个 ctx 上）。
  - 参考：owlet `channel.Bridge.Start(ctx)/Stop`、`loong.web` 组件的 5s 优雅关停均遵循此边界。
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
