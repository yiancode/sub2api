# Sub2API 零停机部署说明

这套方案参考了 `code80` 的“先上传到临时文件，再原子替换”模式，适合把当前仓库部署到：

- 站点目录：`/www/wwwroot/s2.ai80.vip`
- 域名：`https://s2.ai80.vip`

## 适用场景

- 前端通过 `-tags embed` 嵌入到 Go 二进制
- 服务器上只需要运行一个 `sub2api` 主进程
- 想把停机时间控制在上传完成后的数秒内

## 前置条件

首次上线前，服务器至少需要这些条件：

- PostgreSQL 已可用
- Redis 已可用
- 网站已建好，并能为域名配置反向代理
- 已安装宝塔 `Supervisor` 插件，或者你打算用 `systemd`

注意：这个项目不是只有数据库就能跑，`Redis` 也是必需项。首次 Setup Wizard 会要求填写 PostgreSQL 和 Redis。

## 仓库内新增文件

- `deploy/deploy-zero-downtime.sh`
- `deploy/deploy.local.conf.example`
- `deploy/sub2api.bt-supervisor.example.ini`

## 推荐目录约定

### 1. 二进制

部署脚本会把二进制放到：

```bash
/www/wwwroot/s2.ai80.vip/sub2api
```

### 2. 配置文件

推荐通过进程管理器设置：

```bash
DATA_DIR=/etc/sub2api
```

这样首次运行完成 Setup Wizard 后，配置会写到：

```bash
/etc/sub2api/config.yaml
/etc/sub2api/.installed
```

后续重复部署只替换二进制，不会覆盖配置。

## 宝塔 Supervisor 推荐配置

可参考：

```ini
deploy/sub2api.bt-supervisor.example.ini
```

关键点：

- `directory=/www/wwwroot/s2.ai80.vip`
- `command=/www/wwwroot/s2.ai80.vip/run-sub2api.sh`
- `DATA_DIR=/etc/sub2api`
- `SERVER_HOST=127.0.0.1`
- `SERVER_PORT=8526`

建议在宝塔站点中把 `https://s2.ai80.vip` 反向代理到：

```bash
http://127.0.0.1:8526
```

## 首次上线步骤

### 1. 准备本地部署配置

在本地机器上：

```bash
cd deploy
cp deploy.local.conf.example deploy.local.conf
```

至少改这些值：

- `REMOTE_HOST`
- `REMOTE_USER`
- `PROCESS_MANAGER`
- `SUPERVISOR_PROGRAM` 或 `SYSTEMD_SERVICE`
- `SERVER_PORT`

### 2. 配置服务器进程管理

如果你用宝塔：

1. 在 Supervisor 中新增一个 `sub2api` 进程
2. 参考 `deploy/sub2api.bt-supervisor.example.ini`
3. 确认运行目录是 `/www/wwwroot/s2.ai80.vip`
4. 确认 `command` 指向 `/www/wwwroot/s2.ai80.vip/run-sub2api.sh`

`run-sub2api.sh` 会由部署脚本自动生成，里面会显式写死：

- `GIN_MODE=release`
- `SERVER_HOST=127.0.0.1`
- `SERVER_PORT=8526`
- `DATA_DIR=/etc/sub2api`

如果你用 systemd：

1. 参考仓库已有的 `deploy/sub2api.service`
2. 把 `WorkingDirectory` 和 `ExecStart` 改成 `/www/wwwroot/s2.ai80.vip`
3. 最好补上 `Environment=DATA_DIR=/etc/sub2api`

### 3. 执行首次部署

在仓库根目录执行：

```bash
bash deploy/deploy-zero-downtime.sh
```

脚本会：

1. 本地编译前端
2. 本地编译嵌入前端的 Linux 二进制
3. 上传到服务器临时路径
4. 停止旧进程
5. 原子替换 `/www/wwwroot/s2.ai80.vip/sub2api`
6. 启动进程并做健康检查

### 4. 完成首次 Setup Wizard

如果服务器上还没有 `config.yaml`，第一次启动时会进入 Setup Wizard。

此时访问：

```bash
https://s2.ai80.vip
```

按页面提示填写：

- PostgreSQL 连接信息
- Redis 连接信息
- 管理员邮箱和密码

完成后系统会进入正式运行模式。

## 后续更新

以后每次发版，只需要再次执行：

```bash
bash deploy/deploy-zero-downtime.sh
```

如果你已经在本地提前编译好二进制，也可以：

```bash
bash deploy/deploy-zero-downtime.sh --skip-build
```

## 脚本支持的进程管理方式

### supervisor

适合宝塔服务器，默认值就是这个模式。

### systemd

适合标准 Linux 服务器。只需要在 `deploy.local.conf` 里改：

```bash
PROCESS_MANAGER="systemd"
SYSTEMD_SERVICE="sub2api"
```

### nohup

只建议临时调试，不建议长期生产使用。

## 失败回滚

部署脚本会在替换前自动保留：

```bash
/www/wwwroot/s2.ai80.vip/sub2api.backup
```

如果新版本启动失败，脚本会尝试：

1. 恢复 `.backup`
2. 重新启动旧版本

## 常见问题

### 1. 健康检查失败，但进程存在

优先检查：

- 站点反向代理端口是否与 `SERVER_PORT` 一致
- `DATA_DIR` 是否设置正确
- `config.yaml` 是否已经生成
- PostgreSQL / Redis 是否真的可连通

### 2. 第一次启动只看到初始化页面

这是正常现象，说明还没完成 Setup Wizard。

### 3. 只有 PostgreSQL，没有 Redis

不能直接完成上线。请先补 Redis，再做首次初始化。
