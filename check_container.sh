#!/bin/bash
echo "=== 查看所有容器（包括已停止的）==="
docker ps -a | grep jtptllm

echo -e "\n=== 查看容器退出状态 ==="
docker inspect jtptllm --format='{{.State.Status}}: {{.State.ExitCode}}'

echo -e "\n=== 查看容器日志 ==="
docker logs jtptllm

echo -e "\n=== 验证镜像架构 ==="
docker inspect ghcr.io/lim12137/jtptllm:arm64 --format='Architecture: {{.Architecture}}'

echo -e "\n=== 尝试交互式运行以查看错误 ==="
echo "运行: docker run --rm -p 8022:8022 ghcr.io/lim12137/jtptllm:arm64"
