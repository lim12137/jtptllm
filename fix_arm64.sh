#!/bin/bash
echo "正在停止并删除现有容器..."
docker stop jtptllm 2>/dev/null
docker rm jtptllm 2>/dev/null

echo "删除错误的 AMD64 镜像..."
docker rmi ghcr.io/lim12137/jtptllm:latest 2>/dev/null

echo "拉取 ARM64 平台镜像..."
docker pull --platform linux/arm64 ghcr.io/lim12137/jtptllm:latest

echo "运行 ARM64 容器..."
docker run -d -p 8022:8022 --name jtptllm ghcr.io/lim12137/jtptllm:latest

echo "验证容器架构..."
docker inspect jtptllm | grep -A 5 Architecture

echo "查看容器日志..."
docker logs jtptllm

echo "检查容器状态..."
docker ps | grep jtptllm
