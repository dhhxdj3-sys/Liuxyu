# 🌠 流星雨许愿网页 (Liuxyu)

基于 **Vue 3 + Vite** 开发的沉浸式流星雨交互网页。夜幕星空之下，流星划破天际，支持在线写下心愿并寄托给璀璨星空。愿望会通过 **Go + MySQL** 后端保存，刷新页面也不会消失。

---

## 项目特色

- **Vue 3 驱动**：使用 Composition API 构建，轻量高效、响应迅速。
- **流星雨动态效果**：流畅细腻的夜空星光与流星划过动画。
- **双字段许愿**：手机端许愿人与愿望上下双行展示，桌面端并列展示；前后端双重非空校验。
- **愿望持久化**：Go HTTP 服务将愿望写入 MySQL，成功后返回统一 JSON 响应。
- **明确交互反馈**：许愿成功播放流星与横幅，失败展示具体错误信息且保留已填内容。

---

## 技术栈

| 模块 | 技术 |
| --- | --- |
| 前端 | Vue 3（Composition API）、Vite |
| 后端 | Go 标准库 `net/http` |
| 数据库 | MySQL 8.0（`github.com/go-sql-driver/mysql`） |

---

## 目录结构

```text
.
├─ src/App.vue          # 星空动画、许愿表单与接口调用
├─ vite.config.js       # 开发服务器与 /api 代理
├─ server/
│  ├─ main.go           # 服务启动与优雅关闭
│  ├─ config.go         # 读取 .env / 环境变量
│  ├─ handlers.go       # 路由、参数校验、统一响应
│  ├─ store.go          # MySQL 连接、建库与愿望写入
│  ├─ schema.sql        # 建表语句
│  ├─ .env.example      # 连接配置模板
│  ├─ .env              # 本地连接配置（已忽略提交）
│  └─ go.mod
└─ README.md
```

---

## 本地运行

### 1. 配置 MySQL 并启动 Go 后端

要求 Go **1.26+**，本机已启动 **MySQL 8.0**（默认端口 `3306`）。本机 MySQL Workbench 中已有 `Local instance MySQL80` 连接。

先填写数据库连接信息。`server/.env` 已生成并被 Git 忽略，只需补上本机 MySQL 密码：

```ini
MYSQL_HOST=127.0.0.1
MYSQL_PORT=3306
MYSQL_USER=root
MYSQL_PASSWORD=你的MySQL密码
MYSQL_DATABASE=liuxy_wish
PORT=8080
```

> 也可以设置同名系统环境变量，环境变量优先级高于 `.env`。密码只保存在本地，不会提交到仓库。

启动后端：

```sh
cd server
go mod download
go run .
```

服务默认监听 `http://localhost:8080`。首次启动会自动创建数据库 `liuxy_wish` 和表 `wishes`；启动成功后，在 MySQL Workbench 左侧 Schemas 区域刷新，即可看到 `liuxy_wish`。

如果当前 MySQL 账号没有建库权限，可先在 Workbench 中执行下文“建库建表语句”，再启动服务。

如果拉取依赖较慢，可切换 Go 模块代理：

```sh
GOPROXY=https://goproxy.cn,direct go mod download
```

> 修改 `PORT` 后，需要同步修改 `vite.config.js` 中 `/api` 代理的 `API_TARGET`，或在前端使用绝对接口地址（见下文）。

### 2. 启动 Vue 前端

```sh
npm install
npm run dev
```

打开 `http://localhost:5173`。开发环境下前端请求 `/api/wish`，由 Vite 代理到 `http://127.0.0.1:8080`，无需处理跨域。

### 3. 生产构建

```sh
npm run build
```

构建产物在 `dist/`。部署时可让 Web 服务器把 `/api` 反向代理到 Go 服务；也可在构建时指定完整的接口地址：

```sh
VITE_WISH_API_URL=http://localhost:8080/api/wish npm run build
```

> `VITE_*` 变量在构建时写入产物，修改后需要重新构建。Go 服务已开启 CORS，支持前端跨源直连。

---

## 数据库选型与连接方式

**选型：MySQL 8.0。** 本机已有 MySQL 服务与 Workbench，便于直接查看和管理许愿数据；MySQL 的并发写入、权限管理和备份工具也适合正式部署。表使用 `utf8mb4` 字符集，可完整保存中文与 Emoji。

**连接方式：** 后端使用 Go 标准库 `database/sql` + `github.com/go-sql-driver/mysql` 驱动。连接参数从 `.env` 或系统环境变量读取：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `MYSQL_HOST` | `127.0.0.1` | MySQL 地址 |
| `MYSQL_PORT` | `3306` | MySQL 端口 |
| `MYSQL_USER` | `root` | 登录账号 |
| `MYSQL_PASSWORD` | 空 | 登录密码，必须填写 |
| `MYSQL_DATABASE` | `liuxy_wish` | 数据库名 |
| `PORT` | `8080` | Go HTTP 服务端口 |

连接池参数：最大 10 个连接、最大空闲 10 个连接、连接最长复用 3 分钟；时间字段统一使用 UTC。服务启动时会先连接 MySQL 实例，执行 `CREATE DATABASE IF NOT EXISTS`，再选择 `liuxy_wish` 库并执行 `server/schema.sql`。

**建库建表语句**（服务会自动建库；若账号无建库权限，可在 Workbench 手动执行）：

```sql
CREATE DATABASE IF NOT EXISTS liuxy_wish
  CHARACTER SET utf8mb4
  COLLATE utf8mb4_unicode_ci;

USE liuxy_wish;

CREATE TABLE IF NOT EXISTS wishes (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    wisher     VARCHAR(20) NOT NULL,
    wish       VARCHAR(30) NOT NULL,
    created_at DATETIME(6) NOT NULL,
    PRIMARY KEY (id),
    KEY idx_wishes_created_at (created_at DESC),
    CONSTRAINT chk_wishes_wisher_not_empty CHECK (CHAR_LENGTH(TRIM(wisher)) > 0),
    CONSTRAINT chk_wishes_wish_not_empty CHECK (CHAR_LENGTH(TRIM(wish)) > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

三个核心字段：`wisher`（许愿人）、`wish`（愿望）、`created_at`（创建时间，UTC）。`server/schema.sql` 与上面的建表部分保持一致，服务启动时自动执行。

---

## 接口说明

### `POST /api/wish`

请求头：`Content-Type: application/json`

请求体：

```json
{
  "wisher": "颜思雨",
  "wish": "愿家人平安健康"
}
```

字段校验规则（两端一致）：

| 字段 | 必填 | 规则 |
| --- | --- | --- |
| `wisher` | 是 | 去除首尾空格后非空，最多 20 个字符 |
| `wish` | 是 | 去除首尾空格后非空，最多 30 个字符 |

成功响应（HTTP 201）：

```json
{
  "success": true,
  "message": "许愿成功",
  "data": {
    "id": 1,
    "wisher": "颜思雨",
    "wish": "愿家人平安健康",
    "createdAt": "2026-10-21T10:30:00.123456Z"
  }
}
```

失败响应（HTTP 400 / 415 / 500）：

```json
{
  "success": false,
  "message": "许愿人不能为空"
}
```

所有响应统一包含 `success` 与 `message`；成功时额外返回 `data`。常见错误包括：

- `400`：JSON 格式错误、字段缺失或为空、字段超出长度。
- `415`：请求头不是 `application/json`。
- `500`：数据库写入失败（服务端日志记录详细原因）。

### `GET /api/health`

用于检查服务与数据库是否正常：

```json
{
  "success": true,
  "message": "服务正常"
}
```

---

## 前端调用示例

项目中的实际调用位于 `src/App.vue` 的 `sendWish()`：

```js
const response = await fetch('http://localhost:8080/api/wish', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    wisher: '颜思雨',
    wish: '愿家人平安健康'
  })
})

const result = await response.json()
if (!response.ok || result.success !== true) {
  // 展示 result.message，例如“许愿人不能为空”
  throw new Error(result.message)
}
console.log('许愿成功', result.data)
```

开发环境下也可直接使用相对地址 `/api/wish`（推荐，默认即为此配置）：

```js
fetch('/api/wish', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ wisher: '颜思雨', wish: '愿家人平安健康' })
})
```

命令行验证：

```sh
curl -X POST http://localhost:8080/api/wish \
  -H "Content-Type: application/json" \
  -d '{"wisher":"颜思雨","wish":"愿家人平安健康"}'
```

---

## 交互说明

- 手机端（宽度 ≤ 640px）许愿人与愿望上下排列，桌面端保持单行并列。
- “许愿人”和“愿望”均为必填；为空时输入胶囊抖动、对应输入框高亮，并在顶部横幅提示具体缺失项。
- 提交期间按钮禁用并显示等待状态，避免重复写入。
- 后端保存成功后才清空输入、发射许愿流星并显示“许愿成功”横幅。
- 后端返回错误或网络异常时，顶部横幅展示具体原因，已填写内容保持不变，便于重试。
