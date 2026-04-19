# 2026-04-01 GH Actions arm64 产物获取记录

## 目标

- 检查 `gh` 是否已安装并登录
- 查看并尽量触发当前仓库镜像构建 workflow
- 跟踪最新 run 关键状态
- 判断并获取 `arm64` 产物

## 仓库

- Remote: `https://github.com/lim12137/jtptllm`
- Default branch: `main`
- Workflow file: `.github/workflows/docker-build.yml`

## 关键命令与结果摘要

1. `gh --version`
   - 结果: `gh version 2.88.1`

2. `gh auth status`
   - 结果: 已登录 `github.com`，账号 `lim12137`

3. `gh run list --workflow docker-build.yml --limit 5`
   - 结果: 存在近期成功 run，workflow 名为 `docker-build`

4. `gh workflow run docker-build --ref main`
   - 结果: 成功触发新 run
   - Run URL: `https://github.com/lim12137/jtptllm/actions/runs/23849262069`

5. `gh run watch 23849262069 --interval 10 --exit-status`
   - 结果: run `23849262069` 成功完成
   - 关键步骤: `Build and Push` 成功

6. `gh api repos/lim12137/jtptllm/actions/runs/23849262069/artifacts`
   - 结果: 仅有 1 个 artifact
   - Artifact 名称: `lim12137~jtptllm~X03GNM.dockerbuild`
   - 判断: 这是 Docker Build 记录，不是 `linux/arm64` 二进制 artifact

7. `Get-Content -Raw .github/workflows/docker-build.yml`
   - 结果:
   - workflow 会构建 `linux/amd64`、`linux/arm64`、`windows/amd64`
   - 仅上传 `proxy-windows-amd64`
   - Linux 产物未上传为 artifact
   - 同时推送多架构镜像到 `ghcr.io/lim12137/jtptllm:latest`

8. `gh auth token | docker login ghcr.io -u lim12137 --password-stdin`
   - 结果: 登录 GHCR 成功

9. `docker manifest inspect ghcr.io/lim12137/jtptllm:latest`
   - 结果: 多架构 manifest 存在
   - `linux/arm64` digest: `sha256:d3b755f7299952136ed13cf18e8c538f54bf1c248ee6e47b86066a5de183d7a6`

10. `docker pull --platform linux/arm64 ghcr.io/lim12137/jtptllm:latest`
    - 结果: 成功拉取 `linux/arm64` 镜像
    - 最终 digest: `sha256:114c327dca2e339811be3b321e9f4f480e2e076d17ea888d172701f94a447ea8`

11. `docker image inspect ghcr.io/lim12137/jtptllm:latest --format '{{json .Architecture}} {{json .Os}} {{json .Id}}'`
    - 结果: `"arm64" "linux" "sha256:114c327dca2e339811be3b321e9f4f480e2e076d17ea888d172701f94a447ea8"`

12. `docker save -o jtptllm_run23849262069_linux-arm64_image.tar ghcr.io/lim12137/jtptllm@sha256:d3b755f7299952136ed13cf18e8c538f54bf1c248ee6e47b86066a5de183d7a6`
    - 结果: 成功导出 arm64 镜像 tar 到仓库根目录

13. `docker create --platform linux/arm64 --name jtptllm-arm64-export-23849262069 ghcr.io/lim12137/jtptllm@sha256:d3b755f7299952136ed13cf18e8c538f54bf1c248ee6e47b86066a5de183d7a6`
14. `docker cp jtptllm-arm64-export-23849262069:/app/proxy proxy_run23849262069_linux-arm64`
15. `docker rm -f jtptllm-arm64-export-23849262069`
    - 结果: 成功从 arm64 镜像中提取 `/app/proxy`

16. `Get-FileHash jtptllm_run23849262069_linux-arm64_image.tar`
    - SHA256: `A733282DBAAF7066C68675358018160E51C06F32219ED3088203891AF2976FD4`

17. `Get-FileHash proxy_run23849262069_linux-arm64`
    - SHA256: `04888D6D45D6A82111819744679E3D3F65C5FE5F88C87C8EFA674FC134976BA1`

## 输出文件

- 仓库根目录镜像导出: `jtptllm_run23849262069_linux-arm64_image.tar`
- 仓库根目录 arm64 二进制: `proxy_run23849262069_linux-arm64`

## 结论

- `gh` 已安装且已登录。
- `docker-build` workflow 已成功手动触发并执行成功。
- 本 workflow 没有上传 `linux/arm64` 二进制 artifact，只推送了 GHCR 多架构镜像。
- 已通过 GHCR `linux/arm64` 子 manifest 成功导出镜像 tar，并成功提取 arm64 二进制到仓库根目录。
