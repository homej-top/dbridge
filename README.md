<div align="center">

<img src="docs/screenshots/dashboard.png" alt="DBridge Dashboard" width="90%" />

# DBridge - Database Management & Sync Platform

**Go + React | 跨库对比同步 | AI 助手 | 多数据库统一管理**

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go&style=flat-square)](https://go.dev/)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&style=flat-square)](https://react.dev/)
[![TypeScript](https://img.shields.io/badge/TypeScript-5-3178C6?logo=typescript&style=flat-square)](https://www.typescriptlang.org/)
[![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)](LICENSE)
[![PRs Welcome](https://img.shields.io/badge/PRs-welcome-brightgreen?style=flat-square)](http://makeapullrequest.com)

[English](#english) | [中文](#中文)

---

**如果这个项目对你有帮助，请给一个 Star 鼓励一下！**

</div>

---

<a id="中文"></a>

## 中文说明

### 关于 DBridge

DBridge（数桥）是一款面向开发者和 DBA 的 **Web 端数据库管理与同步平台**。一个浏览器搞定 MySQL、PostgreSQL、Oracle、SQL Server 四大主流数据库的日常管理、SQL 查询、结构对比、数据同步和导入导出。

**为什么选择 DBridge？**

- **告别桌面客户端** — 纯 B/S 架构，打开浏览器即用，团队共享同一实例
- **跨库对比同步** — 业界罕见的 MySQL ↔ PostgreSQL ↔ Oracle ↔ SQL Server 结构对比与一键同步能力
- **SQL 方言转换** — 四种数据库 SQL 方言互转，数据库迁移不再手写 SQL
- **AI 深度集成** — 自然语言生成 SQL、SQL 解释、性能优化建议
- **零依赖部署** — 单二进制文件 + SQLite，一条命令即可启动

### 核心功能

| 模块 | 功能 |
|:-----|:-----|
| **数据查询** | Monaco SQL 编辑器、树形 Schema 浏览、表数据查看/编辑/增删、行详情 |
| **数据库管理** | Schema/表/视图 CRUD、DDL 预览、字段结构管理、索引管理 |
| **对比同步** | 跨库表结构对比、差异可视化、结构同步 / 数据同步（全量/增量/差异） |
| **导出导入** | 表数据导出为 SQL、批量导入、跨库数据迁移 |
| **SQL 转换** | MySQL ↔ PostgreSQL ↔ SQL Server ↔ Oracle 方言互转 |
| **AI 助手** | 自然语言 → SQL、SQL 解释、性能优化、多模型配置 |
| **脚本管理** | SQL 脚本目录树管理、Monaco 编辑器、自动保存 |
| **报表面板** | 自定义 SQL 报表，支持表格/柱状图/折线图/饼图 |
| **审计日志** | 全链路操作记录、同步数据归档 |
| **数据源管理** | 多数据源 CRUD、连接测试、AES-256-GCM 密码加密 |

### 支持的数据库

| 数据库 | 版本 | 说明 |
|:-------|:-----|:-----|
| **MySQL** / MariaDB / OceanBase | 5.7+ | TCP 连接 |
| **PostgreSQL** | 12+ | TCP 连接 |
| **SQL Server** | 2012+ | go-mssqldb 驱动 |
| **Oracle** | 11g+ | go-ora 驱动 (Service Name / SID) |

### 截图预览

<div align="center">

**数据查询 — SQL 编辑器 + Schema 树**
<img src="docs/screenshots/data-query-page.png" alt="SQL Editor" width="85%" />

**数据库管理 — 表结构管理**
<img src="docs/screenshots/mysql-db-manage.png" alt="Database Management" width="85%" />

**跨库结构对比 — 差异可视化**
<img src="docs/screenshots/compare-oracle-oracle.png" alt="Structure Compare" width="85%" />

</div>

### 快速开始

#### 环境要求

- **Go** 1.22+（编译后端）
- **Node.js** 18+（编译前端）
- 或直接使用预编译二进制文件

#### 一键构建与启动

```bash
# 1. 克隆项目
git clone https://github.com/homej-top/dbridge.git
cd dbridge

# 2. 构建全栈（后端 + 前端）
./build.sh

# 3. 启动服务
./start.sh

# 4. 访问
#    前端: http://localhost:5170
#    后端: http://localhost:8082
#    默认账号: admin / 123456
```

#### 开发模式

```bash
# 后端（修改代码后执行 ./build.sh && ./restart.sh）

# 前端开发服务器（热更新）
cd web && npm install && npm run dev
# 访问 http://localhost:5170
```

#### Docker 部署

```bash
docker-compose up -d
```

### 技术架构

```
┌─────────────────────────────────────────────────────────┐
│                   浏览器 (React 19 + Ant Design 6)        │
└────────────────────────┬────────────────────────────────┘
                         │ REST API
                         ▼
┌─────────────────────────────────────────────────────────┐
│                   Go 应用服务 (Gin)                       │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐   │
│  │ 认证模块  │ │ 数据源管理│ │ 对比同步  │ │ AI 助手  │   │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘   │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐   │
│  │ SQL 执行  │ │ 表结构管理│ │ 导出导入  │ │ 审计日志  │   │
│  └──────────┘ └──────────┘ └──────────┘ └──────────┘   │
└───────┬──────────────┬──────────────────────────────────┘
        │              │
        ▼              ▼
┌──────────────┐ ┌──────────┐
│ SQLite / PG  │ │  Redis   │
│  (元数据存储) │ │ (缓存/锁) │
└──────────────┘ └──────────┘
```

### 技术栈

| 层级 | 技术 |
|:-----|:-----|
| **后端** | Go 1.22+ / Gin / GORM / JWT / AES-256-GCM / Zap |
| **前端** | React 19 / TypeScript / Vite / Ant Design 6 / Monaco Editor |
| **元数据库** | SQLite（私有化）/ PostgreSQL（SaaS） |
| **部署** | 单二进制 / Docker / Docker Compose |

### 项目结构

```
dbridge/
├── cmd/server/              # 应用入口
├── internal/                # 后端核心代码
│   ├── handler/             # HTTP 处理器 + 路由注册
│   ├── service/             # 业务逻辑层
│   │   └── drivers/         # 数据库驱动 (MySQL/PG/Oracle/MSSQL)
│   ├── repository/          # GORM 数据访问层
│   ├── middleware/           # JWT + RBAC 中间件
│   ├── model/               # 统一响应结构
│   └── config/              # 配置管理
├── pkg/                     # 公共工具包 (加密/日志)
├── web/                     # React 前端
│   └── src/
│       ├── pages/           # 19 个功能页面
│       ├── components/      # 可复用组件
│       ├── api/             # Axios 请求封装
│       ├── locales/         # 国际化 (中/英)
│       └── utils/           # 工具函数
├── configs/config.yaml      # 服务配置
├── test/                    # 集成测试脚本
└── docs/                    # 设计文档
```

### 配置说明

编辑 `configs/config.yaml`：

```yaml
server:
  port: 8082

database:
  type: sqlite              # sqlite 或 postgresql
  sqlite:
    path: ./data/dbbridge.db

jwt:
  secret: change-me-in-production  # 生产环境必须修改
  expires_in: 7200
```

### 常用命令

```bash
./build.sh          # 构建全栈
./start.sh          # 启动服务
./stop.sh           # 停止服务
./restart.sh        # 重启服务
./status.sh         # 查看运行状态
```

### 测试

```bash
cd test && ./run_all.sh           # 运行全部集成测试
./test_01_auth.sh                 # 认证模块
./test_02_datasources.sh          # 数据源模块
./test_03_query.sh                # 查询模块
./test_04_compare.sh              # 对比模块
```

---

<a id="english"></a>

## English

### About DBridge

DBridge is a **web-based database management and synchronization platform** for developers and DBAs. Manage MySQL, PostgreSQL, Oracle, and SQL Server from a single browser interface — with cross-database structure comparison, one-click sync, SQL dialect transpilation, and AI-powered assistance.

**Why DBridge?**

- **No desktop client needed** — Pure B/S architecture, share one instance across your team
- **Cross-database sync** — Rare capability to compare and sync structures across MySQL, PostgreSQL, Oracle, and SQL Server
- **SQL dialect transpilation** — Convert SQL between four major databases instantly
- **AI-native** — Natural language to SQL, SQL explanation, performance optimization
- **Zero-dependency deployment** — Single binary + SQLite, one command to start

### Quick Start

```bash
git clone https://github.com/homej-top/dbridge.git
cd dbridge
./build.sh
./start.sh
# Open http://localhost:5170
# Default credentials: admin / 123456
```

### Features

- **SQL Editor** — Monaco Editor with Schema tree, inline data editing, row details
- **Database Management** — Schema/table/view CRUD, DDL preview, index management
- **Structure Compare & Sync** — Cross-database diff visualization, structure & data sync (full/incremental/diff)
- **Export & Import** — Export to SQL, batch import, cross-database migration
- **SQL Transpilation** — MySQL ↔ PostgreSQL ↔ SQL Server ↔ Oracle
- **AI Assistant** — Text-to-SQL, SQL explanation, optimization suggestions
- **Script Manager** — SQL script tree, Monaco editor, auto-save
- **Reports & Dashboards** — Custom SQL reports with charts
- **Audit Logs** — Full operation tracking, sync data archiving
- **Security** — JWT auth, RBAC, AES-256-GCM password encryption

### Supported Databases

| Database | Version | Driver |
|:---------|:--------|:-------|
| MySQL / MariaDB / OceanBase | 5.7+ | go-sql-driver |
| PostgreSQL | 12+ | lib/pq |
| SQL Server | 2012+ | go-mssqldb |
| Oracle | 11g+ | go-ora |

### Tech Stack

| Layer | Technology |
|:------|:-----------|
| **Backend** | Go 1.22+ / Gin / GORM / JWT / AES-256-GCM / Zap |
| **Frontend** | React 19 / TypeScript / Vite / Ant Design 6 / Monaco Editor |
| **Metadata DB** | SQLite (standalone) / PostgreSQL (SaaS) |
| **Deployment** | Single binary / Docker / Docker Compose |

---

## 贡献指南

欢迎贡献！请参考 [CONTRIBUTING.md](CONTRIBUTING.md) 了解开发流程和代码规范。

1. Fork 本仓库
2. 创建功能分支 (`git checkout -b feature/amazing-feature`)
3. 提交更改 (`git commit -m 'Add amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 提交 Pull Request

---

## License

[MIT](LICENSE) © 2026 DBridge

---

<div align="center">

**DBridge — 让数据库管理更简单**

如果这个项目帮到了你，请给我们一个 Star，你的支持是我们持续改进的动力！

</div>
