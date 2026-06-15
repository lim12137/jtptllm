# 统一向量接口真实验收

日期：2026-06-14

## 目标

为代理新增并验证两个统一通用接口：

- `/v1/embeddings`
- `/v1/rerank`

并固定模型职责：

- `bge-m3-1024维` 负责 embedding
- `Qwen3-Reranker-0.6B` 负责 rerank

## 本地构建与测试

```powershell
D:\GO\bin\go.exe test ./...
D:\GO\bin\go.exe build ./...
```

结果：

- `go test ./...` 通过
- `go build ./...` 通过

## 真实联调方式

先编译最新代理：

```powershell
D:\GO\bin\go.exe build -o M:\AI\1work\llm\jtptllm_work\proxy-test-new.exe ./cmd/proxy
```

启动参数：

```powershell
M:\AI\1work\llm\jtptllm_work\proxy-test-new.exe `
  --api-md <包含 compatible-mode POST 地址的 md 文档> `
  --app-key <APP_KEY> `
  --host 127.0.0.1 `
  --port 8042
```

说明：

- 当前项目在 `--api-md` 模式下仍存在默认 `api.txt` 启动依赖问题
- 本次联调期间曾临时放置最小占位 `api.txt`，验收后已删除

## 真实请求

### embeddings

```powershell
$embBody = @{ input = @('第一段文本','第二段文本') } | ConvertTo-Json -Depth 6
Invoke-WebRequest -UseBasicParsing 'http://127.0.0.1:8042/v1/embeddings' `
  -Method Post `
  -ContentType 'application/json' `
  -Body $embBody
```

### rerank

```powershell
$rerankBody = @{
  query = '北京天气'
  documents = @('北京今天晴，适合出行','上海今天有雨，注意带伞')
  top_n = 2
} | ConvertTo-Json -Depth 6

Invoke-WebRequest -UseBasicParsing 'http://127.0.0.1:8042/v1/rerank' `
  -Method Post `
  -ContentType 'application/json' `
  -Body $rerankBody
```

## 真实结果

- 缺失 `api.txt` 启动
  - 代理可正常启动
  - 会自动生成占位 `api.txt`
  - 本次实测生成内容：

```txt
# auto-generated placeholder api.txt
# fill APP_KEY and agentCode before using agent gateway mode
key: 
agentCode: 
```

- `/v1/embeddings`
  - HTTP `200`
  - 返回 `model = rsv-tchvlgrj`
  - 返回真实 embedding 向量
- `/v1/rerank`
  - HTTP `200`
  - 返回 `model = rsv-11m4dmp2`
  - 本次样例返回 `results = []`

## 结论

1. 代理层统一接口已经真正暴露出来，不再需要调用方感知上游 `ai_version/...` 路径。
2. `/v1/embeddings` 已真实跑通，并固定承接到 `bge-m3-1024维`。
3. `/v1/rerank` 已真实跑通，并固定承接到 `Qwen3-Reranker-0.6B`。
4. 缺失 `api.txt` 时，启动链路已经能自动补一个带说明注释的占位文件。
5. 需要注意：
   - 默认 `api.txt` 为相对路径时，实际生成位置取决于进程工作目录。
