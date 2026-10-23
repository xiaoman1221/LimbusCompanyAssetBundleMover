# Limbus Company AssetBundle Mover

将 Limbus Company 的 AssetBundle 缓存迁移到其他盘符，并在原位置创建目录链接，
在释放 C 盘空间的同时保证游戏仍能正常读取资源。

默认源目录：`C:\Users\<用户名>\AppData\LocalLow\Unity\ProjectMoon_LimbusCompany`

## 功能

- [x] 自绘深色主窗体（中文提示）
- [x] 自动检测源目录、建议目标盘、显示磁盘剩余空间
- [x] 异步扫描目录，统计占用大小与文件数
- [x] 启动时检测 Limbus Company 进程，确认后可结束进程
- [x] 迁移：复制 → 校验 → 备份源目录 → 创建目录链接 → 删除备份
- [x] 启动时检测目录链接；已链接时可校验、移动数据并重新链接到新目录
- [x] 校验：检查链接指向与文件数/总大小是否与迁移记录一致
- [x] 还原：复制回源目录 → 校验 → 删除目标数据与迁移记录
- [x] 迁移/重新链接/还原按需请求管理员权限（启动时不需要）

## 已链接时的启动提示

启动时如果检测到源目录已经是链接，会弹出提示面板：

- **移动到新目录**：选择一个新的空目录，迁移数据并重建链接
- **还原到原位置**：把数据复制回源目录，删除链接与目标数据
- **暂不处理**：关闭提示，之后可在主界面点击「重新链接到新目录」或「还原」

选择「暂不处理」不会修改任何文件。

## 权限说明

- 程序以普通权限启动（清单为 `asInvoker`）
- 点击「迁移到目标目录」或「还原」时才会弹出 UAC，由提权后的子进程执行文件操作
- 「校验」是只读操作，不会提权
- 优先创建目录符号链接；若当前进程没有创建链接的权限，则退化为目录联接（Junction）

迁移记录保存在：`%LOCALAPPDATA%\LimbusCompanyAssetBundleMover\state.json`

## 构建

```bash
go build -ldflags "-H windowsgui" -o LimbusCompanyAssetBundleMover.exe .
```

`rsrc.syso` 内嵌了 `app.manifest`（启用 Common Controls 6、DPI 感知与 asInvoker 权限）。
修改 manifest 后重新生成：

```bash
go run github.com/akavel/rsrc@latest -manifest app.manifest -o rsrc.syso
```

缺少该资源时，walk 在 Windows 11 上会因 `TTM_ADDTOOL failed` 崩溃。

## 测试

```bash
go test .
```

## 自动发布

`.github/workflows/release.yml` 会在向 `master` / `main` 推送提交时扫描本次推送的 commit message。
只要任意一条提交信息包含 `vX.Y.Z`（例如 `release v1.0.0`），就会：

1. 交叉编译 Windows x64（`GOOS=windows`、`GOARCH=amd64`）
2. 生成带版本号的 exe 与 `SHA256SUMS.txt`
3. 创建同名 tag / GitHub Release（如 `v1.0.0`）并上传产物

提交信息中没有版本号时只执行检测步骤，不构建、不发布。

