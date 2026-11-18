# Git Glance

Git Glance 让你一眼看清 git 仓库的所有分支：每个分支合进了哪些主干、落后多少、哪些可以安全对齐。它由 Go 核心、网页界面、VS Code 插件和浏览器模式组成。

[English](README.md)

## 从 VSIX 安装

构建或下载对应平台的 VSIX（`git-glance-<平台>-<架构>-<版本>.vsix`），然后：

```
code --install-extension git-glance-win32-x64-0.1.0.vsix
```

点击活动栏里的 Git Glance 图标即可使用。要卸载，在扩展视图里卸载即可。

## 环境要求

- 需要 Git 2.30 或更新的版本。git 版本更旧或没有装时，Git Glance 不做任何改动：不能添加仓库、不能读线上、不能对齐、不动文件夹，并在工具栏下方说明原因。
- 带工作文件夹的仓库。暂不支持裸仓库（bare repository）。

## 浏览器模式

```
gitglance serve
```

核心会在本机启动一个网页服务，并打印形如 `http://127.0.0.1:<端口>/#token=...` 的链接。令牌是这次运行 `gitglance serve` 时生成的，放在 `#` 之后的片段里，不会出现在发给服务器的请求行中；页面在这个浏览器标签页的会话期间保存它，并放在请求头里发送。用浏览器打开链接即可。可选参数：`--port N`、`--no-open`。

## 从源码构建

需要：Go（版本见 `core/go.mod`）、Node.js 22 及以上。

```
cd ui && npm ci
cd ../vscode && npm ci
cd ..
node scripts/build.mjs all --targets host
```

`scripts/build.mjs <ui|core|vsix|all> [--targets host|all]` 把核心程序输出到 `dist/core/`，VSIX 输出到 `dist/`。`--targets all` 会编出 Windows、macOS、Linux 各 x64 和 arm64。

## Git Glance 绝不会做的事

- 绝不推送，绝不删除或修改远程分支，对远程只做 `fetch`。打开仓库、读取失败后点重试、以及对齐时，会运行 `git fetch --all --prune --no-prune-tags`：它更新远程跟踪分支（`refs/remotes/...`），并删掉远程上已删除的分支对应的远程跟踪分支；和普通的 `git fetch` 一样，可能会新增指向所取历史的标签。它不改你自己的分支，也不删除任何标签。
- 只做快进（fast-forward）。
- 拒绝处理有未提交、未跟踪或隐藏（assume-unchanged / skip-worktree）改动的文件夹。
- 绝不删除分支。

## 安全说明

- **删除文件夹**（删除工作树文件夹）会先把被忽略的文件备份，构建产物文件夹除外：`node_modules`、`target`、`build`、`dist`、`out`、`.gradle`、`__pycache__`、`.pytest_cache`、`.mypy_cache`、`.tox`、`.next`、`.nuxt`、`coverage`、`.venv`、`venv`；备份成功后才删除。删除前会再检查一遍：分支和提交和确认弹窗显示的一样、没有未提交的改动、没有进行中的操作（rebase、merge、cherry-pick、revert、bisect）、构建产物文件夹里没有独立的 git 仓库、没有只在这个文件夹里的提交、文件夹本身不是链接。备份之后再检查一次：被忽略文件的清单和每个备份文件的大小与修改时间、分支和提交、进行中的操作、“忽略改动”标记、只在这个文件夹里的提交、构建产物里有没有 git 仓库、文件夹是不是变成了链接；有任何变化就什么都不删。构建产物文件夹里的改动不检查（反正会删掉）。就在这一刻被改动的已跟踪或未跟踪文件会让 git 拒绝删除，这时文件夹里的链接可能已经删掉了（只删链接本身，从不动它指向的内容）。备份放在 `<Git Glance 主目录>/backups/` 下：主目录是环境变量 `GITGLANCE_HOME`（若设置），否则是用户配置目录里的 `git-glance`（Linux 上如 `~/.config/git-glance`，macOS 上 `~/Library/Application Support/git-glance`，Windows 上 `%AppData%\git-glance`）。要清理备份，删掉这个 `backups` 文件夹即可。
- 你仓库自己的钩子（hooks）和 fsmonitor 会和你自己用 git 时一样运行。
- Git Glance 把自己的设置、操作记录和备份放在上面的主目录里，不会在你的仓库里写它自己的文件。它对仓库做的改动，只有你点击的那些 git 操作。

## 许可

MIT
