# HTTPS Proxy | HTTPS 代理

<p align="center">
  <a href="#english">English</a> | <a href="#chinese">中文</a>
</p>

<a name="english"></a>

## Overview

HTTPS Proxy is a secure, certificate-based authentication proxy server for controlling and monitoring HTTPS connections. It provides detailed traffic statistics and an admin dashboard for real-time monitoring.

![Admin Dashboard](docs/images/admin_dashboard.png)

## Features

- ✅ Client certificate-based authentication
- ✅ TLS certificate chain verification against trusted CA
- ✅ Certificate usage verification for client authentication
- ✅ Detailed user traffic statistics tracking
- ✅ Web-based admin dashboard
- ✅ Bilingual interface (English/Chinese)
- ✅ Multiple deployment options (service, Docker)
- ✅ Cross-platform support (Linux, macOS, Windows)

## Installation

### Prerequisites

- Go 1.19 or higher (for building from source)
- OpenSSL (for certificate generation)

### Quick Install with Script

```bash
# For Linux (requires sudo)
curl -sSL https://raw.githubusercontent.com/razeencheng/https-proxy/main/scripts/install.sh | sudo bash

# For macOS (requires sudo)
curl -sSL https://raw.githubusercontent.com/razeencheng/https-proxy/main/scripts/install.sh | sudo bash
```

### Manual Installation

1. Download the latest release for your platform from [GitHub Releases](https://github.com/razeencheng/https-proxy/releases)
2. Extract the archive and run the installation script:

```bash
# Linux
sudo ./deploy/install_linux.sh

# macOS
sudo ./deploy/install_macos.sh
```

### Docker Deployment

```bash
# Clone the repository
git clone https://github.com/razeencheng/https-proxy.git
cd https-proxy

# Build and start the container
docker-compose up -d
```

## Configuration

The configuration is stored in `config.json` (development) or `/etc/https-proxy/config.json` (production).

### Configuration Structure

```json
{
  "server": {
    "address": "0.0.0.0:8443",
    "cert_file": "./certs/cert.pem",
    "key_file": "./certs/key.pem",
    "language": "en"
  },
  "proxy": {
    "trust_root_file": "./certs/trustroot.pem",
    "auth_required": true
  },
  "stats": {
    "enabled": true,
    "save_interval": 300,
    "file_path": "./stats/proxy_stats.json"
  },
  "admin": {
    "enabled": true,
    "address": "127.0.0.1:8444",
    "cert_file": "./certs/admin_cert.pem",
    "key_file": "./certs/admin_key.pem"
  }
}
```

### Key Configuration Options

| Section | Option | Description |
|---------|--------|-------------|
| server | address | Proxy server listening address and port |
| server | language | UI language: 'en' for English, 'zh' for Chinese |
| proxy | auth_required | Enable/disable client certificate verification |
| stats | save_interval | How often to save statistics (seconds) |
| admin | address | Admin dashboard listening address and port |

## Certificate Management

For testing, generate self-signed certificates:

```bash
./scripts/generate_certs.sh
```

This creates:
- CA certificate (ca.pem)
- Server certificates (cert.pem/key.pem)
- Admin certificates (admin_cert.pem/admin_key.pem)
- Client certificate (client.pem/client.key)
- Browser-importable PKCS#12 file (client.p12)

For production, use your trusted CA certificates.

## Using the Proxy

### Client Setup

1. Import the client certificate (client.p12) into your browser
2. Configure your browser to use the proxy (default: localhost:8443)
3. When prompted, select your client certificate

### Admin Dashboard

Access the admin dashboard at https://localhost:8444 (or configured address).

The dashboard provides:

- Real-time connection statistics
- Per-user traffic monitoring
- Historical data visualization

![User Details](docs/images/user_details.png)

## API Reference

The admin panel provides the following API endpoints:

- `GET /api/stats`: Get statistics for all users
- `GET /api/stats/user/{username}`: Get statistics for a specific user
- `GET /api/config`: Get server configuration

## Uninstallation

```bash
# Linux
sudo ./deploy/uninstall_linux.sh

# macOS
sudo ./deploy/uninstall_macos.sh
```

## Development

### Building from Source

```bash
# Clone the repository
git clone https://github.com/razeencheng/https-proxy.git
cd https-proxy

# Build the binary
go build -ldflags="-w -s" -o https-proxy

# Run with default config
./https-proxy
```

### Creating Releases

This project uses GitHub Actions to automatically build and release binaries:

1. Tag the commit: `git tag -a v1.0.0 -m "Version 1.0.0"`
2. Push the tag: `git push origin v1.0.0`

## License

This software is provided under the terms of the MIT License.

---

<a name="chinese"></a>

# HTTPS 代理

## 概述

HTTPS 代理是一个安全的基于证书认证的代理服务器，用于控制和监控 HTTPS 连接。它提供详细的流量统计和管理员仪表板，以进行实时监控。

![管理员仪表板](docs/images/admin_dashboard.png)

## 特性

- ✅ 基于客户端证书的身份验证
- ✅ 对受信任 CA 的 TLS 证书链验证
- ✅ 针对客户端身份验证的证书用途验证
- ✅ 详细的用户流量统计跟踪
- ✅ 基于 Web 的管理仪表板
- ✅ 双语界面（英文/中文）
- ✅ 多种部署选项（系统服务、Docker）
- ✅ 跨平台支持（Linux、macOS、Windows）

## 安装

### 前提条件

- Go 1.19 或更高版本（用于从源代码构建）
- OpenSSL（用于证书生成）

### 使用脚本快速安装

```bash
# Linux（需要 sudo）
curl -sSL https://raw.githubusercontent.com/razeencheng/https-proxy/main/scripts/install.sh | sudo bash

# macOS（需要 sudo）
curl -sSL https://raw.githubusercontent.com/razeencheng/https-proxy/main/scripts/install.sh | sudo bash
```

### 手动安装

1. 从 [GitHub Releases](https://github.com/razeencheng/https-proxy/releases) 下载适合您平台的最新版本
2. 解压缩归档文件并运行安装脚本：

```bash
# Linux
sudo ./deploy/install_linux.sh

# macOS
sudo ./deploy/install_macos.sh
```

### Docker 部署

```bash
# 克隆仓库
git clone https://github.com/razeencheng/https-proxy.git
cd https-proxy

# 构建并启动容器
docker-compose up -d
```

## 配置

配置存储在 `config.json`（开发环境）或 `/etc/https-proxy/config.json`（生产环境）中。

### 配置结构

```json
{
  "server": {
    "address": "0.0.0.0:8443",
    "cert_file": "./certs/cert.pem",
    "key_file": "./certs/key.pem",
    "language": "en"
  },
  "proxy": {
    "trust_root_file": "./certs/trustroot.pem",
    "auth_required": true
  },
  "stats": {
    "enabled": true,
    "save_interval": 300,
    "file_path": "./stats/proxy_stats.json"
  },
  "admin": {
    "enabled": true,
    "address": "127.0.0.1:8444",
    "cert_file": "./certs/admin_cert.pem",
    "key_file": "./certs/admin_key.pem"
  }
}
```

### 主要配置选项

| 部分 | 选项 | 描述 |
|------|------|------|
| server | address | 代理服务器监听地址和端口 |
| server | language | UI 语言：'en' 为英文，'zh' 为中文 |
| proxy | auth_required | 启用/禁用客户端证书验证 |
| stats | save_interval | 保存统计数据的频率（秒） |
| admin | address | 管理仪表板监听地址和端口 |

## 证书管理

对于测试，生成自签名证书：

```bash
./scripts/generate_certs.sh
```

这将创建：
- CA 证书 (ca.pem)
- 服务器证书 (cert.pem/key.pem)
- 管理员证书 (admin_cert.pem/admin_key.pem)
- 客户端证书 (client.pem/client.key)
- 可导入浏览器的 PKCS#12 文件 (client.p12)

对于生产环境，使用您的受信任 CA 证书。

## 使用代理

### 客户端设置

1. 将客户端证书 (client.p12) 导入到您的浏览器中
2. 配置您的浏览器使用代理（默认：localhost:8443）
3. 提示时，选择您的客户端证书

### 管理仪表板

通过 https://localhost:8444（或配置的地址）访问管理仪表板。

仪表板提供：

- 实时连接统计
- 按用户流量监控
- 历史数据可视化

![用户详情](docs/images/user_details.png)

## API 参考

管理面板提供以下 API 端点：

- `GET /api/stats`：获取所有用户的统计信息
- `GET /api/stats/user/{username}`：获取特定用户的统计信息
- `GET /api/config`：获取服务器配置

## 卸载

```bash
# Linux
sudo ./deploy/uninstall_linux.sh

# macOS
sudo ./deploy/uninstall_macos.sh
```

## 开发

### 从源代码构建

```bash
# 克隆仓库
git clone https://github.com/razeencheng/https-proxy.git
cd https-proxy

# 构建二进制文件
go build -ldflags="-w -s" -o https-proxy

# 使用默认配置运行
./https-proxy
```

### 创建发布版本

该项目使用 GitHub Actions 自动构建和发布二进制文件：

1. 标记提交：`git tag -a v1.0.0 -m "Version 1.0.0"`
2. 推送标签：`git push origin v1.0.0`

## 许可证

本软件根据 MIT 许可证条款提供。
