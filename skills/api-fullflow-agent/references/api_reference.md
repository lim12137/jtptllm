# Agent 网关 API 参考（createSession/run/feedback/deleteSession）

本文件根据你项目目录里的 PDF 文档整理，供脚本与 Skill 使用。

## 基础信息

- 默认 Base URL（可用环境变量/参数覆盖）：
  - `http://10.54.102.36:80/xlm-gateway--vinrl/sfm-api-gateway/gateway/agent/api`
- 认证：HTTP Header `Authorization: Bearer <APP_KEY>`
- 内容类型：`Content-Type: application/json`

## 配置文件 api.txt（推荐）

`api.txt` 至少包含 3 个字段：

```txt
key： <APP_KEY>
agentCode： <agentCode>
agentVersion： <agentVersion>
```

说明：分隔符支持半角/全角冒号（`:` / `：`），允许有空格。

## 1) 创建会话 createSession

- 方法：POST
- 路径：`/createSession`
- Body：

```json
{
  "agentCode": "xxxx",
  "agentVersion": "xxxx"
}
```

- Response（示例）：

```json
{
  "success": true,
  "data": {
    "uniqueCode": "4bcaa882-9c54-4b78-9057-54db58591b5b",
    "errorCode": null,
    "errorMsg": null
  }
}
```

`uniqueCode` 即后续调用里的 `sessionId`。

## 2) 发起智能体调用 run

- 方法：POST
- 路径：`/run`
- Body：

```json
{
  "stream": true,
  "delta": true,
  "sessionId": "来自 createSession 的 uniqueCode",
  "trace": false,
  "message": {
    "text": "用户输入",
    "metadata": {},
    "attachments": [
      {
        "url": "https://...",
        "name": "可选文件名"
      }
    ]
  }
}
```

### run 的输出（非流式）

非流式一般返回 JSON：

```json
{
  "success": true,
  "data": {
    "message": {
      "role": "assistant",
      "content": [
        {
          "type": "text",
          "text": { "value": "智能体输出文本" }
        }
      ],
      "metadata": null
    },
    "thoughts": null,
    "error": null,
    "errorCode": null,
    "errorMsg": null
  }
}
```

### run 的输出（流式）

流式输出通常是按行推送，常见形式类似 SSE：每行以 `data:` 开头，后面是 JSON。

示例（单行）：

```txt
data: {"object":"message.delta","end":false,"role":"assistant","sessionId":"...","content":{"type":"text","text":{"value":"增量文本"}}}
```

当 `end=true`（或出现 `data: [DONE]`）表示结束。

## 3) 调用结果反馈 feedback（可选）

- 方法：POST
- 路径：`/feedback`
- Body（示例，字段以实际返回为准）：

```json
{
  "sessionId": "会话ID",
  "requestId": "请求ID",
  "taskId": "任务ID",
  "uniqueCode": "可选：唯一ID",
  "subject": "TASK",
  "provider": { "source": "USER", "extendInfo": {} },
  "vote": "LIKE",
  "extCommentsInfo": { "comment": "123" }
}
```

注意：`requestId/taskId` 通常需要从 `run` 的返回/流式事件的 `metadata` 中提取。

## 4) 删除会话 deleteSession

- 方法：POST
- 路径：`/deleteSession`
- Body：

```json
{ "sessionId": "会话ID" }
```

- Response（示例）：

```json
{ "success": true, "data": true }
```

## 常见错误码（节选）

HTTP 401：
- `GATEWAY APP_KEY MISSING!` 缺少 APP_KEY
- `GATEWAY APP_KEY WRONG!` APP_KEY 无效
- `GATEWAY APP_KEY ALREADY EXPIRED!` APP_KEY 已过期
- `GATEWAY ROLE NOT MATCH!` 角色不匹配
- `GATEWAY ROLE HAS EXPIRED!` 角色已过期
- `GATEWAY AUTH TYPE WRONG!` 鉴权方式不支持
- `GATEWAY AUTH_USER NOT MATCH WRONG!` 用户信息不匹配
- `GATEWAY AUTH_USER_ROLE WRONG!` 用户角色不匹配

HTTP 403：
- `GATEWAY APP PATH NOT FOUND!` 无效的服务调用路径
- `GATEWAY APP PATH NOT REGISTER!` 路径未注册
- `GATEWAY HEADER PARAMETER MISSING!` 业务所需 header 缺失
- `GATEWAY PARAMETER WORKSPACE NO MATCH!` 工作空间与 APP_KEY 不匹配

HTTP 404：
- `GATEWAY ROUTE URL NOT FOUND!` 目标地址无效

HTTP 429：
- `GATEWAY LIMIT !` 服务触发限流

HTTP 500：
- `TARGET_SERVICE_ERROR_CONNECTION_REFUSE_EXCEPTION` 目标服务拒绝连接
- `TARGET_SERVICE_ERROR_NO_RESPONSE_EXCEPTION` 目标服务无响应
