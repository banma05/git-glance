# Git Glance

Git Glance 让你一眼看清 git 仓库的所有分支：每个分支合进了哪些主干、落后多少、哪些可以安全对齐。它由 Go 核心、网页界面、VS Code 插件和浏览器模式组成。

[English](README.md)

## 从 VSIX 安装

构建或下载对应平台的 VSIX（`git-glance-<平台>-<架构>-<版本>.vsix`），然后：

```
code --install-extension git-glance-win32-x64-0.1.0.vsix
```

点击活动栏里的 Git Glance 图标即可使用。要卸载，在扩展视图里卸载即可。

## 浏览器模式

```
gitglance serve
```

核心会在本机启动一个网页服务，并打印形如 `http://127.0.0.1:<端口>/#token=...` 的链接。一次性令牌放在 `#` 之后的片段里，不会出现在发给服务器的请求行中。用浏览器打开链接即可。可选参数：`--port N`、`--no-open`。

## 从源码构建

需要：Go（版本见 `core/go.mod`）、Node.js 20 及以上。

```
cd ui && npm ci
cd ../vscode && npm ci
cd ..
node scripts/build.mjs all --targets host
```

`scripts/build.mjs <ui|core|vsix|all> [--targets host|all]` 把核心程序输出到 `dist/core/`，VSIX 输出到 `dist/`。`--targets all` 会编出 Windows、macOS、Linux 各 x64 和 arm64。

## Git Glance 绝不会做的事

- 绝不推送，绝不删除或修改远程分支，对远程只做 `fetch`。
- 只做快进（fast-forward）。
- 拒绝处理有未提交、未跟踪或隐藏（assume-unchanged / skip-worktree）改动的文件夹。
- 绝不删除分支。

## 安全说明

- **删除文件夹**（删除工作树文件夹）会先把被忽略的文件备份，`node_modules`、`dist`、`target` 这类构建产物除外；备份成功后才删除。备份放在 `<Git Glance 主目录>/backups/` 下：主目录是环境变量 `GITGLANCE_HOME`（若设置），否则是用户配置目录里的 `git-glance`（Linux 上如 `~/.config/git-glance`，macOS 上 `~/Library/Application Support/git-glance`，Windows 上 `%AppData%\git-glance`）。要清理备份，删掉这个 `backups` 文件夹即可。
- 你仓库自己的钩子（hooks）和 fsmonitor 会和你自己用 git 时一样运行。
- Git Glance 不在你的仓库里写任何文件；设置和操作记录都在上面的主目录里。

## 许可

MIT
