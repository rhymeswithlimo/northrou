<div align="center">
<img alt="Northrou" src="public/repo/Hero_Banner_JPG_v1.2__Northrou.jpg" width="100%">
</div>

<h3 align="center">Northrou</h3>

<p align="center">
  <a href="README.md">English</a> ·
  简体中文 ·
  <a href="README.es.md">Español</a> ·
  <a href="README.fr.md">Français</a> ·
  <a href="README.de.md">Deutsch</a> ·
  <a href="README.ja.md">日本語</a>
</p>

<p align="center">你的电影和剧集，从你自己的硬件直接串流播放。</p>

<p align="center">
  <a href="https://northrou.sh">网站</a> ·
  <a href="https://northrou.sh/docs">文档</a> ·
  <a href="#安装">安装</a> ·
  <a href="#许可证">许可证</a>
</p>

<p align="center">
<a href="https://github.com/rhymeswithlimo/northrou/releases"><img src="https://img.shields.io/github/v/release/rhymeswithlimo/northrou" alt="Latest release"></a>
<a href="LICENSE"><img src="https://img.shields.io/badge/license-BSD--3--Clause-blue" alt="License: BSD 3-Clause"></a>
<a href="https://github.com/rhymeswithlimo/northrou/commits/main"><img src="https://img.shields.io/github/last-commit/rhymeswithlimo/northrou" alt="Last commit"></a>
<img src="https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.26+">
</p>

---

Northrou 是一款开源媒体服务器，运行在你自己的硬件上。只需指向你的电影和剧集库，它就能将内容串流到你的手机、平板、电脑或电视，无论在家还是在外，你的媒体内容不会经过任何第三方服务器。

播放会根据正在观看的设备自动调整。只要设备能够直接处理，文件就会原封不动地播放，只有在真正需要时才会进行转码，并在有可用 GPU 时加以利用。杜比全景声和无损音轨会按设备原样传递或适配，而不是被压缩成立体声。

添加一个媒体库，剩下的交给 Northrou：海报、演员表和详细信息会自动匹配，字幕（包括大多数服务器无法处理的图像字幕）开箱即用，而基于你自己观看历史构建的推荐引擎（从不对外分享）会帮你找到下一部想看的内容。

由一个人完成一次服务器设置，然后分享一个连接码。其他人只需在应用中输入这个连接码即可连接：无需账号、无需邮箱、无需密码。远程访问采用点对点方式：你的服务器和设备直接通信，中间没有任何一方能看到你正在播放的内容。

## 安装

```sh
curl -sSL https://raw.githubusercontent.com/rhymeswithlimo/northrou/main/scripts/install.sh | sh
northrou setup
```

安装脚本会将 Northrou 设置为后台服务，并自动获取 FFmpeg，不需要再安装其他任何东西。接下来 `setup` 会在终端里引导你为服务器命名、添加媒体文件夹并生成连接码，全程无需浏览器。在其他设备上安装应用、输入连接码，即可完成连接。

更喜欢使用 Docker，或者想手动安装？完整的安装说明（包括所有安装方式和配置选项）在 [northrou.sh/docs](https://northrou.sh/docs)，也可以查看本仓库中的 [docs/](docs/) 目录。

## 命令

日常使用中你基本不需要用到这些命令：如果想深入了解运行状况，`northrou admin` 会打开一个实时终端面板，显示当前的串流、硬件和容量情况。

```text
northrou <command> [flags]

命令：
   setup                    在终端中完成服务器设置（名称、媒体、TMDB 密钥、连接码）
   status                   显示服务器当前状态以及下一步该做什么
   doctor                   检查配置并报告任何问题
   serve                    在前台运行服务器（服务实际调用的命令）
   install / uninstall      注册 / 移除系统服务
   start / stop / restart   控制已安装的服务
   logs                     显示或跟踪服务器最近的日志输出
   admin                    打开实时管理面板（TUI）
   scan [path...]           立即扫描指定文件夹或磁盘（不指定路径则扫描已配置的目录）
   match <file>             将文件强制匹配到指定的 TMDB 条目
   cc                       打印当前服务器的连接码（用于配对应用）
   cc rotate                更换连接码，并使所有设备下线
   devices                  列出已配对的设备
   devices revoke <id>      将某个已配对设备下线
   tmdb-key                 查看、设置或删除 TMDB API 密钥
   update                   检查并安装新版本
   version                  打印版本信息
   -h, --help               显示某个命令的帮助信息

全局参数（适用于所有命令）：
   --config string          config.toml 的路径（默认：系统配置目录）
   -v, --verbose             启用调试日志

日志参数：
   -f, --follow             持续输出新增加的日志行
   -n, --lines int          显示的末尾行数（默认 200）

管理面板参数：
   --addr string            服务器的基础 URL（默认取自配置，例如 http://localhost:8674）

扫描参数：
   --tv                     将指定路径视为剧集（默认：根据文件名自动判断）

匹配参数：
   --tmdb-id int            要关联的电影或剧集的 TMDB ID（必填）
   --tv                     将该文件视为一集剧集
   --season int             季数（需配合 --tv）
   --episode int             集数（需配合 --tv）

更换连接码参数：
   -y, --yes                无需确认直接更换

更新参数：
   -y, --yes                无需确认直接安装更新
   --check                  仅检查，不安装
```

`setup` 是交互式的，不需要任何参数。服务相关命令（`install`、`start`、`stop`、`restart`、`update`）会写入 root 权限的位置，因此在 Linux 上需要用 `sudo` 运行。命令会在需要提权时提示你。

## 文档

完整的参考文档，包括所有配置选项、HTTP API、架构说明等，都在 [northrou.sh/docs](https://northrou.sh/docs)。这些页面在本仓库中也有对应的镜像：

- [配置参考](docs/configuration.md)
- [HTTP API 参考](docs/api.md)
- [架构](docs/architecture.md)
- [客户端](docs/frontend.md)

## 开发

Northrou 完全开源，你可以自行构建。它是一个 monorepo：服务器和远程访问代理是两个独立的 Go 模块，而客户端（`frontend/`）是一个在 Web、桌面、iOS 和 Android 上共用的 Tauri 应用。

```sh
make build   # 构建客户端，然后生成 bin/northrou 和 bin/coordinator
make test    # 运行测试套件
make run     # 构建并在本地运行服务器
```

各部分如何协同工作，参见 [docs/architecture.md](docs/architecture.md)。

## 许可证

BSD 3-Clause 许可证，详见 [LICENSE](LICENSE)。在该许可证条款下，你可以自由构建、运行、fork 并重新分发本软件。**Northrou** 名称、标志和品牌资产不在授权范围内，未经许可不得用于为衍生产品背书或宣传（详见 [NOTICE](NOTICE)）。
