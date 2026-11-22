# FrameFlow · 帧流

用 Vue 3 和 Go 实现的短视频 Feed 原型。访客可以直接播放内置视频，按标签浏览；注册邮箱账号后，可以发布 MP4、点赞、收藏、关注作者、留言、回复并给评论点赞。

## 启动与体验

**本机已配置的原型**在项目根目录运行 `./scripts/run-local.sh`，浏览器打开 [帧流](http://127.0.0.1:8080)。该脚本使用本地配置启动 Feed API 和互动服务，按 Ctrl-C 结束本次运行。

**新环境**安装 Docker Compose 和 OpenSSL 后，在仓库根目录执行：

```sh
./scripts/compose-up.sh
docker compose logs -f feed interaction
```

首次启动会生成仅保存在本机的 `.env`，初始化两个 MySQL 库并导入三条示例视频。网页入口同上。运行 `docker compose stop` 停止，`docker compose start` 恢复。MySQL 和上传目录各自使用持久卷。

**体验顺序。** 打开“发现”，用手机上下滑或桌面方向键切换视频。注册账号 A，选择“发布”，上传一段 MP4 并填写标签。用另一个浏览器窗口的独立会话注册账号 B，打开该视频、点赞、回复评论，然后刷新查看结果。“我赞过的”和“我的收藏”各自按最近操作排序。

## 服务与数据流

```text
Vue 3 / TypeScript ──HTTP──► feed-api（Gin）──gRPC──► interaction-service（Go）
                              │                          │
                              ├─ MySQL · Feed 库          └─ MySQL · 互动库
                              ├─ Redis · 公共首页视频 ID
                              └─ 本地 MP4 / 封面 / 上传目录
```

| 入口 | 实现 | 职责 |
| --- | --- | --- |
| 浏览器 | `web/src/` | Vue 3、TypeScript 和 Vite；播放器切换、标签筛选、账号、发布和互动面板 |
| Feed HTTP 服务 | `cmd/feed-api/`、`internal/feed/` | 邮箱鉴权、上传、分页、播放统计、批量组装互动信息及静态媒体传输 |
| 互动 gRPC 服务 | `cmd/interaction-service/`、`internal/interaction/` | 关系写入、批量状态查询、评论树和评论点赞 |
| 服务契约 | `proto/interaction/v1/interaction.proto` | 仓库包含生成的 Go 客户端和服务端，可直接构建 |
| 数据表与种子 | `db/feed.sql`、`db/interaction.sql`、`db/seed.sql` | 可执行表结构和三条有本地媒体文件的示例数据；业务模型见[数据库表设计](数据库表设计.md) |

**账号与会话。** 以规范化邮箱注册，密码用 bcrypt 哈希。登录后签发随机会话令牌，通过 HttpOnly Cookie 传递；MySQL 的 `sessions` 表只保存令牌哈希，七天过期。退出时删除对应会话。Feed API 从会话取用户 ID 后调用 gRPC。访客可读公开 Feed，播放次数只计入登录用户。

**首页与 Redis。** 按发布时间、视频 ID 倒序做游标分页。无标签、无游标且页大小为 8 的公共首页使用 Redis Key `frameflow:feed:latest:first:8`，缓存有序 ID（含用于判定下一页的一条记录），TTL 为 30 秒。Feed 服务批量补齐仍可见的视频和标签，每页只发一次 `BatchGetInteractions`。缓存未命中或无法访问时查询 MySQL；发布事务成功后删除首页缓存。用户自己的点赞、收藏和关注结果按当前账号查询。

**gRPC 边界。** Feed 服务只访问 Feed 库，互动服务只访问互动库。首页查询互动数据的截止时间为 750 毫秒；互动服务不可用时仍返回视频，`interaction_available=false`，界面隐藏未知数值并禁用互动写入。点赞、收藏、关注和评论点赞通过 `enabled` 表达最终状态，联合主键防止重复写入。评论及回复共用一张表，互动服务校验父评论所属视频并推导 `root_id`。一级评论倒序，展开回复后正序分页。

**发布与媒体。** 原型将最大 64 MiB 的 MP4 保存到本地目录，支持浏览器按 HTTP Range 读取。Feed API 检查文件头、生成随机文件名，把视频元数据和标签关联放在同一个 MySQL 事务中；写入失败时清理本次上传。内置视频带静态封面，用户上传的视频由播放器直接读取。SeaweedFS 预签名直传和 RabbitMQ 异步生成封面属于后续接入项。示例素材的来源和许可见[媒体署名](media/CREDITS.md)。

## HTTP 接口

以下路径以 `/api` 为前缀。列表响应使用 `items` 和 `next_cursor`；翻页时原样传回游标。

| 方法与路径 | 用途 |
| --- | --- |
| `POST /register`、`POST /login`、`POST /logout`、`GET /me` | 注册、登录、退出及当前身份 |
| `GET /feed?limit=8&tag=动画` | 公开 Feed，可选标签及 `cursor` |
| `POST /videos` | multipart 表单上传，字段为 `file`、`caption` 和逗号分隔的 `tags` |
| `PUT /videos/:id/like`、`PUT /videos/:id/favorite`、`PUT /users/:id/follow` | JSON 传入目标状态，如 `{"enabled":true}` |
| `GET /likes`、`GET /favorites` | 当前用户赞过或收藏的视频 |
| `GET /videos/:id/comments`、`POST /videos/:id/comments` | 列表查询可带 `root_id` 展开回复；发布时可带 `parent_id` 回复一条具体留言 |
| `PUT /comments/:id/like` | 给某条评论或回复设置点赞状态 |
| `POST /videos/:id/plays`、`POST /videos/:id/plays/:playID/five-seconds` | 播放开始时获取 `play_id`，达到五秒后使用同一凭据上报 |
| `GET /health` | HTTP 进程存活检查 |

## 播放统计口径

同一账号在同一自然日观看同一条视频，最多贡献一次浏览和一次五秒播放。

| 视频累计字段 | 计数时机 |
| --- | --- |
| `videos.view_count` | 播放器实际开始播放。仅在列表中曝光或预加载不计入。 |
| `videos.view_5s_count` | 同一次播放实际观看达到 5 秒。暂停、缓冲、拖动跳过的时长不计入，多次短播放的时长也不合并。 |

例如，用户第一次看了 2 秒，浏览量加 1。当天回来又看了 8 秒，只有五秒播放次数加 1。之后当天再看，两项累计值都保持不变。

## MySQL 如何去重并计数

Feed 服务在 `frameflow_feed.video_watch_daily` 保存近期的观看状态。每行对应一个日期、一个视频和一个账号，联合主键为 `(stat_date, video_id, user_id)`。`stat_date` 按服务端记录的播放开始时间和统一的业务时区确定。`watch_stage` 取 `1`（已开始播放）或 `2`（已达到五秒）；尚无记录表示当天还没有计数。

播放器用同一个 `play_id` 标识一次播放及其重试。服务端核实事件后，按以下规则推进当日状态：

| 当前状态 | 收到的事件 | 新状态 | 浏览量增量 | 五秒播放增量 |
| --- | --- | --- | ---: | ---: |
| 无记录 | 开始播放 | 1 | 1 | 0 |
| 无记录 | 达到五秒，开始事件迟到 | 2 | 1 | 1 |
| 1 | 达到五秒 | 2 | 0 | 1 |
| 已达到或超过上报阶段 | 重试、迟到或再次观看 | 保持原值 | 0 | 0 |

观看状态与 `videos` 的两个累计值在同一个 MySQL 事务内更新。联合主键保证同一天只有一条对应记录，事务内的行锁保证并发请求依次判断状态。只有成功跨过门槛的请求才增加相应的累计值。

## 跨日补报与清理

新的一天使用新的 `stat_date`，前一天的记录可以继续接收跨日补报。例如，用户在 23:59:58 开始播放，次日 00:00:03 看满五秒，这两个事件都归属开始播放的那一天。

1. 配置补报截止时间，当前实现允许前一天的播放补报至次日 02:00，默认业务时区为 Asia/Shanghai，可用 STAT_TIMEZONE 修改。
2. 到期后，先拒绝归属该日期的新上报，等已受理的写入完成，再按 `stat_date` 分批删除过期明细。当天的数据继续正常使用。
3. 视频累计值已在播放时写入 `videos`，清理明细后仍保留。如果以后需要按日查询历史数据，在删除明细前按日期和视频保存日汇总。

播放器只在有登录用户、视频可见且正常播放时累计时长，并排除跳进度、暂停及缓冲造成的时间差。服务端签发 HMAC 播放凭据，绑定用户、视频和开始时间，拒绝未满五秒或凭据不匹配的上报。实际观看由浏览器报告；凭据与数据库约束用于处理时间门槛、重复请求及并发。清理任务在启动时和之后每 15 分钟运行，每批最多删除 500 条过期明细。

## 非容器开发与验证

已有 Go 1.24+、Node.js 18.20+、MySQL 8.0 时，也可以分别构建。MySQL 初始化脚本使用 InnoDB，批量互动查询使用 `JSON_TABLE`。本机原型库名为 `frameflow_proto_feed` 和 `frameflow_proto_interaction`，各服务连接账号只获得自己库的读写权限。

```sh
(cd web && npm ci && npm run build)
mkdir -p .frameflow/bin
go build -tags nomsgpack -o .frameflow/bin/feed-api ./cmd/feed-api
go build -o .frameflow/bin/interaction-service ./cmd/interaction-service
```

自行配置本地环境时，先创建这两个空库，分别导入 `db/feed.sql` 和 `db/interaction.sql`，再向 Feed 库导入 `db/seed.sql`。将自己的连接参数写入 `.frameflow/local.env`（仅当前用户可读）。启动脚本加载这个文件：

```sh
FEED_DSN='feed用户名:密码@tcp(127.0.0.1:3306)/frameflow_proto_feed'
INTERACTION_DSN='互动用户名:密码@tcp(127.0.0.1:3306)/frameflow_proto_interaction'
PLAY_SECRET='替换为 openssl rand -hex 32 生成的独立随机值'
REDIS_ADDR=127.0.0.1:6379
GIN_MODE=release
```

HTTP 默认监听 `127.0.0.1:8080`，gRPC 默认监听 `127.0.0.1:9091`。Redis 可选，留空 `REDIS_ADDR` 可运行 MySQL 路径。本机已配置的演示用 Redis 端口为 `16379`。开发前端时，可在后端运行期间执行 `cd web && npm run dev`，Vite 将 API 和媒体请求代理到本机 Feed API。

**验证命令。** 两个带 DSN 的数据库测试仅在提供对应变量时执行。HTTP 验收会创建两个测试账号和两条上传记录，适合在独立测试库运行。

```sh
go test -tags nomsgpack ./...
go vet -tags nomsgpack ./...
FEED_TEST_DSN="$FEED_DSN" FRAMEFLOW_TEST_INTERACTION_DSN="$INTERACTION_DSN" \
  go test -count=1 -tags nomsgpack ./internal/feed ./internal/interaction
BASE_URL=http://127.0.0.1:8080 python3 scripts/smoke.py
```

本机使用 MySQL 8.0.41 完成了并发重复点赞、播放阶段乱序及重复上报的集成测试，并通过双账号 HTTP 验收，覆盖 MP4 上传与 Range 读取、标签分页、互动关系、嵌套回复和五秒播放。已实测 Redis 写入及命中，并验证 Redis 与互动服务同时不可达时依旧返回视频列表。三段内置视频都通过 HTTP Range 校验和解码验证。前端通过 TypeScript 检查及 Vite 生产构建。Docker Compose 配置供新环境启动，本机验收使用原生进程。
