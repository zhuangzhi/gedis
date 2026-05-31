# Gedis v0.1.0 发布指南

本指南介绍如何为 gedis 库发布 v0.1.0 版本。

## 📋 已完成的准备工作

✅ go.mod 包名已更新为 `github.com/gedis/gedis`
✅ .gitignore 文件已创建
✅ CHANGELOG.md 已添加
✅ 所有单元测试通过 (67个)
✅ 性能基准测试可用
✅ 文档已齐全 (README, API, PERSISTENCE_DESIGN)
✅ MIT 许可证

---

## 🚀 发布前最后准备

### 1. 初始化 Git 仓库

如果你还没有初始化 git 仓库：

```powershell
cd d:\app\gedis

# 初始化 git
git init

# 配置用户信息（如果未配置）
git config user.name "你的名字"
git config user.email "你的邮箱"

# 添加所有文件
git add .

# 创建初始提交
git commit -m "Initial commit - gedis v0.1.0"
```

### 2. 创建 GitHub 仓库

在 GitHub 上创建一个名为 `gedis` 的公开仓库。然后添加远程仓库：

```powershell
git remote add origin https://github.com/<你的用户名>/gedis.git

# 推送到 GitHub（分支名通常是 main 或 master）
git branch -M main
git push -u origin main
```

### 3. 创建和推送标签

```powershell
# 创建版本标签
git tag -a v0.1.0 -m "gedis v0.1.0 release"

# 推送标签到 GitHub
git push origin v0.1.0
```

### 4. 创建 GitHub Release

1. 访问你的 GitHub 仓库页面：https://github.com/<你的用户名>/gedis
2. 点击 **"Releases"** 标签
3. 点击 **"Draft a new release"**
4. 选择 **"Choose a tag"** 并选择 `v0.1.0`
5. 填写：
   - **Release title**: `v0.1.0 - Initial Release`
   - **Description**: 粘贴 CHANGELOG.md 中 v0.1.0 的内容
6. 点击 **"Publish release"**

---

## 📝 发布后验证

### 验证包是否可安装

在另一个项目中测试安装：

```powershell
mkdir test-project
cd test-project
go mod init test-project

# 测试安装 gedis
go get github.com/gedis/gedis@v0.1.0
```

### 验证基本用法

创建 `test.go`：

```go
package main

import (
    "fmt"
    "github.com/gedis/gedis"
)

func main() {
    db := gedis.New()
    
    val := gedis.Buf("world")
    db.Set("hello", val)
    val.Close()
    
    v, ok := db.Get("hello")
    if ok {
        fmt.Println(v.String()) // world
        v.Close()
    }
    
    fmt.Println("gedis v0.1.0 安装成功！")
}
```

运行：

```powershell
go run test.go
```

---

## 📦 包发布检查清单

| 项 | 完成状态 |
|-----|--------|
| 所有代码已提交 | ⬜ |
| go.mod 包名正确 | ✅ |
| go.sum 已更新 | ⬜ |
| 标签已创建并推送 | ⬜ |
| GitHub Release 已发布 | ⬜ |
| 在 pkg.go.dev 可检索到 | ⬜ |

---

## 💡 后续发布流程

对于未来的版本发布，遵循此流程：

1. 确保所有更改已提交到 `main` 分支
2. 更新 CHANGELOG.md
3. 运行测试确保通过：`go test -v ./...`
4. 创建并推送标签：
   ```
   git tag -a v0.1.1 -m "gedis v0.1.1"
   git push origin v0.1.1
   ```
5. 在 GitHub 上创建新的 Release

---

## 📚 相关参考

- [如何发布 Go 模块](https://go.dev/doc/modules/publishing)
- [语义化版本控制](https://semver.org/lang/zh-CN/)
- [如何在 GitHub 上创建 Release](https://docs.github.com/zh/repositories/releasing-projects-on-github/managing-releases-in-a-repository)
