# Nav Tool

个人自托管书签导航。左侧多级分类，右侧紧凑书签网格。Go + SQLite + React + TypeScript，MIT 开源。

## 本地运行

需要 Go 1.26+（go.mod 固定工具链）和 Node.js 22.12+。Node.js 只在开发、构建和浏览器测试时需要。

```sh
cd web
npm ci
npm run build
cd ..
go build -buildvcs=false -trimpath -ldflags='-s -w' -o bin/navtool ./cmd/navtool
./bin/navtool
```

默认监听 `http://127.0.0.1:31506`。

## 首次创建管理员与初始化凭据

1. 启动应用。只有数据库尚未创建管理员时，程序才会自动生成初始化凭据，写入权限为 `0600` 的 `data/setup-token`，无需手工创建。
2. 在 VPS 终端读取凭据：

```sh
cat /home/fstar/project/nav_tool/data/setup-token
```

3. 在本地浏览器打开 `https://nav.345001.xyz`，将文件内容填写到“初始化凭据”。
4. 设置用户名（默认显示 `admin`，可以修改）和自己的密码（至少 10 字节），点击“创建导航”。没有预置用户名密码组合。
5. 创建成功后自动登录，凭据文件自动删除。以后使用设置的用户名和密码登录，不再需要初始化凭据。

如果使用其他 `NAV_DATA_DIR`，凭据位于该数据目录下的 `setup-token`。管理员已经存在时，不会重新生成凭据；不要删除数据库来重建凭据。不要将初始化凭据粘贴到公开渠道。主页和数据接口需要登录。

## 去掉导入的外层分类

“收藏夹栏”等浏览器外层文件夹会按原样导入。若希望去掉这一层，可在编辑模式中将其子分类的“上级分类”改为“顶级分类”，将直属书签移入保留的分类，再删除已经清空的外层分类。

不要直接删除整个分类的数据；非空分类在界面上也会禁止直接删除。批量调整前先备份。原始 HTML 仍含有外层文件夹时，再次导入会重新创建这层路径。

## 使用

- 左侧分类筛选自身及子分类书签，搜索跨全部分类匹配名称、网址和备注。
- 编辑模式下拖动书签或同级分类调整顺序；书签还提供前移、后移按钮。拖拽把手支持空格拾取、方向键移动、空格放下。
- 编辑书签可更换所属分类。非空分类不能直接删除，分类移动禁止循环。
- 导入 Edge/Chrome 导出的 HTML，先预览再确认。保留多级分类及顺序，同一分类内相同网址跳过，跨分类允许保留。仅允许 HTTP/HTTPS 链接；根层书签归入“未分类”。
- 网址比较会忽略协议和主机大小写及根路径的 `/`，保留其他路径、查询参数和片段；同名同父分类在导入时合并。
- 导出的内嵌 PNG/JPEG/GIF/WebP/ICO 图标保存为独立文件。缺失图标使用名称占位，可在编辑书签时手动更新网站 favicon；获取限制为公共网络 80/443 端口。导入不会批量联网抓取。
- 设置与备份中可手动备份、设置每天执行时间、时区与保留份数，并下载历史归档。

服务停止时，也可用 `./bin/navtool -import /path/to/favorites.html` 导入初始数据。该命令同样按分类去重，不会创建默认管理员。

## 本地备份与恢复

数据目录包含 `nav.db`、`icons/` 和 `backups/`。定时备份默认关闭，开启后默认每天 03:00（Asia/Shanghai），保留 7 份，可在后台调整。定时调度由应用进程执行，应用需保持运行；配置和最近一次执行状态持久化。

备份使用 `VACUUM INTO` 创建一致性快照，并执行 `PRAGMA integrity_check`。快照和复制图标期间阻止应用修改操作，浏览仍可继续。打包后原子保存至 `data/backups/`，成功后清理旧版本。归档包括数据库、图标和版本说明。应用设置在数据库中；部署参数见下表，迁移时需另行保存。

异地存储暂未实现，扩展点为 `internal/app/backup.go` 中的 `BackupStore`。当前只有本地文件系统实现。

手动备份（建议服务运行时使用后台按钮，避免额外进程同时修改数据）：

```sh
./bin/navtool -data data -backup
```

恢复命令只接受**尚不存在的新目标目录**，检查归档路径、数据库完整性、外键和图标引用，并清除旧登录会话：

```sh
./bin/navtool restore -archive /path/to/nav-backup.tar.gz -to /path/to/new-data
```

在独立端口检查恢复结果，确认后停止正式服务，再将服务的数据目录切换到恢复目录。首次部署及备份逻辑或数据结构重大变更后应实际恢复验证。不要直接用运行中数据库的文件复制作为备份。

## VPS 部署

已约定域名 `https://nav.345001.xyz`，用户提供的现有 Caddy 路由为 `reverse_proxy localhost:31506`。无需改动其他站点。应用仅监听回环地址，不直接开放公网端口。

| 环境变量 | 默认值 | 用途 |
| --- | --- | --- |
| `NAV_ADDR` | `127.0.0.1:31506` | 监听地址 |
| `NAV_DATA_DIR` | `data` | 持久化数据目录 |
| `NAV_WEB_DIR` | `web/dist` | 前端构建目录 |
| `NAV_ORIGIN` | 空 | 正式访问来源，生产设置为 `https://nav.345001.xyz`，同时启用 Secure Cookie |

当前采用 **systemd 用户服务**，使用 `deploy/nav-tool-user.service`，路径和运行用户按本机 `fstar` 配置。重新部署时，在完成构建后以 `fstar` 执行：

```sh
cd /home/fstar/project/nav_tool
mkdir -p data ~/.config/systemd/user
cp deploy/nav-tool-user.service ~/.config/systemd/user/nav-tool.service
systemctl --user daemon-reload
systemctl --user enable --now nav-tool.service
```

本机服务已安装并启用，`fstar` 的 linger 也已开启，无需重复设置。新主机要让用户服务在开机且无人登录时运行，需要执行一次以下命令（sudo 会提示输入密码）：

```sh
sudo loginctl enable-linger fstar
loginctl show-user fstar -p Linger
systemctl --user is-enabled nav-tool.service
```

预期分别显示 `Linger=yes` 和 `enabled`。这仍是用户级服务，无需改成系统级服务或重启 VPS。已验证当前配置和访问正常，尚未实际重启 VPS 验证。

日常管理以 `fstar` 执行，不加 sudo：

```sh
systemctl --user status nav-tool.service
systemctl --user restart nav-tool.service
journalctl --user -u nav-tool.service -n 50 --no-pager
```

更新 Go 可执行文件后重启服务；仅更新前端静态文件无需重启。`deploy/nav-tool.service` 是备用的系统级服务示例，本机未采用；不要同时启用两份服务占用同一端口。

Docker 部署可使用 `docker compose up -d --build`，配置以非 root 运行、删除 capabilities、只读根文件系统，端口仅绑定 `127.0.0.1:31506`。数据使用命名卷；不挂载 Docker socket。弱 VPS 推荐在开发机完成构建。Docker 镜像构建需要网络，本地不代表已经验证镜像构建。

生产开启前验证 DNS、TLS、Caddy 上游、UFW 和监听端口，保留 SSH 访问。备份文件及图标仅通过认证接口提供，数据目录不作为静态目录暴露。

## 开发与验证

```sh
go test ./...
go vet ./...
cd web
npm run build
npx playwright install chromium
npm run test:e2e
```

前端开发 `npm run dev`，Vite 将 `/api` 代理到本地 31506，运行后端时可不设置 `NAV_ORIGIN`；浏览器开发地址与 API 同源。

Playwright 使用独立临时数据目录与 31516 端口，不改正式数据。后端测试可读取本机 `docs/favorites_2026_11_9.html` 实际验证导入；私人书签文件已忽略，不在仓库中分发，缺失时跳过该项，其余合成导入测试仍执行。

设计和验收清单见 [docs/design.md](docs/design.md)，实现和运行限制见 [docs/implementation.md](docs/implementation.md)。
