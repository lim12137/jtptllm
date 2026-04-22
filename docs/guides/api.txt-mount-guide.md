# api.txt 运行时挂载指南

## 说明

从 v2.0 开始，Docker 镜像**不再包含 api.txt 文件**。配置文件应该在容器运行时从宿主机挂载，这样可以：

- ✅ 同一镜像在不同环境使用不同配置
- ✅ 避免敏感配置进入镜像层
- ✅ 符合 12-Factor 应用的配置分离原则
- ✅ 更新配置无需重新构建镜像

## 挂载方式

### 方式 1: Docker CLI

```bash
# 绝对路径挂载
docker run -d \
  --name proxy \
  -v /etc/proxy/api.txt:/app/api.txt:ro \
  -p 8022:8022 \
  ghcr.io/your-org/proxy:latest

# 相对路径挂载 (从当前目录)
docker run -d \
  --name proxy \
  -v $(pwd)/api.txt:/app/api.txt:ro \
  -p 8022:8022 \
  ghcr.io/your-org/proxy:latest
```

### 方式 2: Docker Compose

创建 `docker-compose.yml`:

```yaml
version: '3.8'

services:
  proxy:
    image: ghcr.io/your-org/proxy:latest
    container_name: proxy
    ports:
      - "8022:8022"
    volumes:
      - ./api.txt:/app/api.txt:ro
    environment:
      - GOMEMLIMIT=512MiB
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "/app/proxy", "-health"]
      interval: 30s
      timeout: 3s
      retries: 3
      start_period: 5s
```

启动：

```bash
docker-compose up -d
```

### 方式 3: Kubernetes ConfigMap

创建 ConfigMap:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: proxy-config
  namespace: default
data:
  api.txt: |
    # 在这里粘贴你的 api.txt 内容
    # 例如:
    # key1=value1
    # key2=value2
```

应用 ConfigMap:

```bash
kubectl apply -f configmap.yaml
```

创建 Deployment:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: proxy
spec:
  replicas: 1
  selector:
    matchLabels:
      app: proxy
  template:
    metadata:
      labels:
        app: proxy
    spec:
      containers:
      - name: proxy
        image: ghcr.io/your-org/proxy:latest
        ports:
        - containerPort: 8022
        volumeMounts:
        - name: config-volume
          mountPath: /app/api.txt
          subPath: api.txt
          readOnly: true
        resources:
          limits:
            memory: "512Mi"
            cpu: "500m"
      volumes:
      - name: config-volume
        configMap:
          name: proxy-config
          items:
          - key: api.txt
            path: api.txt
```

或者使用外部文件创建 ConfigMap:

```bash
kubectl create configmap proxy-config \
  --from-file=api.txt=./api.txt \
  -n default
```

### 方式 4: Kubernetes Secret (推荐用于敏感配置)

```bash
kubectl create secret generic proxy-config \
  --from-file=api.txt=./api.txt \
  -n default
```

Deployment 中使用 Secret:

```yaml
volumes:
- name: config-volume
  secret:
    secretName: proxy-config
    items:
    - key: api.txt
      path: api.txt
```

## 验证挂载

### 检查文件是否存在

```bash
docker exec proxy cat /app/api.txt
```

### 检查权限

```bash
docker exec proxy ls -la /app/api.txt
# 应该显示只读权限
```

### 检查应用日志

```bash
docker logs proxy
# 确认应用成功加载配置
```

## 更新配置

### Docker

```bash
# 1. 修改宿主机的 api.txt 文件
vim api.txt

# 2. 重启容器
docker restart proxy
```

### Kubernetes ConfigMap

```bash
# 1. 更新 ConfigMap
kubectl create configmap proxy-config \
  --from-file=api.txt=./api.txt \
  -n default \
  --dry-run=client -o yaml | \
  kubectl apply -f -

# 2. 重启 Pod
kubectl rollout restart deployment/proxy
```

## 安全建议

1. **文件权限**: 使用 `:ro` (只读) 挂载，防止容器内修改
2. **Secret 管理**: 敏感配置使用 Kubernetes Secret 而非 ConfigMap
3. **访问控制**: 限制 ConfigMap/Secret 的访问权限
4. **审计日志**: 记录配置变更历史

## 故障排查

### 问题 1: 容器启动失败，找不到 api.txt

**检查挂载路径**:
```bash
docker inspect proxy | grep -A 10 Mounts
```

**确保护宿机文件存在**:
```bash
ls -la ./api.txt
```

### 问题 2: 配置文件内容为空

**检查 ConfigMap 内容**:
```bash
kubectl get configmap proxy-config -o yaml
```

**重新创建 ConfigMap**:
```bash
kubectl delete configmap proxy-config
kubectl create configmap proxy-config --from-file=api.txt=./api.txt
```

### 问题 3: 权限错误

确保使用正确的用户运行容器：
```yaml
securityContext:
  runAsUser: 65532  # nonroot
  runAsGroup: 65532
```

## 迁移指南

### 从旧镜像迁移

如果你之前使用的是包含 api.txt 的旧镜像：

```bash
# 1. 备份当前的 api.txt
docker exec proxy cat /app/api.txt > api.txt.backup

# 2. 停止旧容器
docker stop proxy && docker rm proxy

# 3. 拉取新镜像
docker pull ghcr.io/your-org/proxy:latest

# 4. 使用挂载方式启动新容器
docker run -d \
  --name proxy \
  -v $(pwd)/api.txt.backup:/app/api.txt:ro \
  -p 8022:8022 \
  ghcr.io/your-org/proxy:latest
```

## 本地开发

开发环境下，可以直接挂载本地文件：

```bash
# 使用脚本
./scripts/restart_proxy.ps1

# 或手动执行
docker run -d \
  --name proxy-dev \
  -v $(pwd)/api.txt:/app/api.txt:ro \
  -v $(pwd)/logs:/app/logs \
  -p 8022:8022 \
  ghcr.io/your-org/proxy:latest
```

## 参考

- [Docker 卷挂载文档](https://docs.docker.com/storage/volumes/)
- [Kubernetes ConfigMap](https://kubernetes.io/docs/concepts/configuration/configmap/)
- [Kubernetes Secrets](https://kubernetes.io/docs/concepts/configuration/secret/)
- [12-Factor 应用配置](https://12factor.net/config)
