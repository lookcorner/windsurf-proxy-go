// Package management provides user-friendly log translation.
package management

import (
	"regexp"
	"strings"
	"time"
)

// User-friendly log message translations (English pattern -> Chinese translation)
var userLogTranslations = []struct {
	pattern     string
	translation string
}{
	// Startup related
	{"Application startup complete", "服务启动成功"},
	{"Server starting on", "服务监听在"},
	{"Health checks started", "健康检查已启动"},
	{"Config loaded", "配置加载成功"},
	{"Added local instance", "添加本地实例"},
	{"Added manual instance", "添加手动配置实例"},
	{"Added standalone instance", "添加独立实例"},
	{"Added instance", "添加实例"},
	{"Created instance", "创建实例"},
	{"Config saved to", "配置已保存到"},
	{"No config path set", "未设置配置路径"},
	{"Migrating config from", "配置文件迁移"},
	{"Config file.*not found", "配置文件未找到，使用默认配置"},
	{"Configuration persistence path", "配置文件路径"},

	// Connection related
	{"Instance.*recovered", "实例已恢复连接"},
	{"Instance.*unhealthy", "实例连接失败"},
	{"Instance.*healthy", "实例健康状态正常"},
	{"Starting language_server", "语言服务器启动中"},
	{"Language server ready", "语言服务器就绪"},
	{"Language server started", "语言服务器已启动"},
	{"Language server stopped", "语言服务器已停止"},

	// Request related
	{"\\[API\\] Request:", "请求处理"},
	{"\\[API\\] Complete:", "请求完成"},
	{"\\[API\\] Stream complete", "流式响应完成"},
	{"\\[Balancer\\] Selected instance", "选择实例"},
	{"\\[Cascade\\] started", "Cascade 会话已开始"},
	{"\\[Cascade\\] message sent", "消息已发送"},
	{"\\[Cascade\\] content stabilized", "内容已稳定，结束会话"},
	{"\\[Cascade\\] auto-continue", "自动继续对话"},
	{"\\[Cascade\\] tool_call", "工具调用"},
	{"\\[Cascade\\] tool_result", "工具执行结果"},
	{"\\[Cascade\\] poll", "轮询状态"},
	{"\\[Cascade\\] user_text", "用户输入"},
	{"\\[Cascade\\] start failed", "启动失败"},
	{"\\[Cascade\\] send failed", "发送失败"},
	{"\\[Cascade\\] poll failed", "轮询失败"},
	{"\\[Cascade\\] error", "Cascade 错误"},
	{"\\[gRPC\\] ChatStream", "gRPC 请求"},
	{"Processing request", "处理请求"},
	{"Forwarding to", "转发到"},
	{"duration=", "耗时"},
	{"tokens=", "令牌数"},
	{"Request: model=", "请求模型"},
	{"Instance.*failed", "实例失败"},
	{"Instance.*restarted", "实例已重启"},
	{"\\[调度\\]", "调度"},
	{"\\[请求\\]", "请求"},
	{"开始处理请求", "开始处理请求"},
	{"完成，模型", "完成，模型"},
	{"转发到.*节点", "转发到节点"},

	// Auth related
	{"Firebase login successful", "Firebase 登录成功"},
	{"Logging in via Firebase", "正在通过 Firebase 登录"},
	{"Token refresh", "Token 刷新"},
	{"API key loaded", "API Key 加载成功"},
	{"CSRF token extracted", "CSRF Token 提取成功"},
	{"Discovered gRPC port", "发现 gRPC 端口"},

	// Error related - keep original for debugging but add prefix
	{"Error:", "错误"},
	{"failed", "失败"},
	{"marked unhealthy", "标记为不健康"},
}

// Technical log patterns to filter out (not shown to users)
var technicalPatterns = []string{
	"[gRPC]",
	"[protobuf]",
	"[Tool parse]",
	"[Cascade] poll #",
	"[LS:",
	"wire",
	"varint",
	"field_number",
	"step_type",
	"grpc_frame",
	"encode",
	"decode",
}

// Log level translations
var logLevelCN = map[string]string{
	"INFO":    "信息",
	"WARNING": "警告",
	"ERROR":   "错误",
	"DEBUG":   "调试",
}

// ShouldShowLog determines if a log should be shown to users.
func ShouldShowLog(message string) bool {
	msgLower := strings.ToLower(message)

	// Filter out technical details
	for _, tech := range technicalPatterns {
		if strings.Contains(message, tech) || strings.Contains(msgLower, strings.ToLower(tech)) {
			return false
		}
	}

	return true
}

// TranslateLogMessage translates a log message to user-friendly Chinese.
func TranslateLogMessage(message string) string {
	result := message

	// Apply translations
	for _, t := range userLogTranslations {
		re, err := regexp.Compile(t.pattern)
		if err != nil {
			continue
		}
		if re.MatchString(message) {
			result = re.ReplaceAllString(result, t.translation)
		}
	}

	return result
}

// TranslateLogLevel translates log level to Chinese.
func TranslateLogLevel(level string) string {
	if cn, ok := logLevelCN[level]; ok {
		return cn
	}
	return level
}

// FormatLogForUser formats a log entry for WebSocket broadcast to users.
// Returns nil if the log should not be shown.
func FormatLogForUser(level string, message string) map[string]interface{} {
	// Filter technical logs
	if !ShouldShowLog(message) {
		return nil
	}

	// Translate message and level
	translatedMsg := TranslateLogMessage(message)
	translatedLevel := TranslateLogLevel(level)

	return map[string]interface{}{
		"time":    time.Now().Format("15:04:05"),
		"level":   translatedLevel,
		"message": translatedMsg,
	}
}

// BroadcastUserLog sends a translated log to WebSocket clients.
func (h *Handler) BroadcastUserLog(level string, message string) {
	logEntry := FormatLogForUser(level, message)
	if logEntry == nil {
		return // Don't broadcast technical logs
	}

	h.logClientsMu.Lock()
	for conn := range h.logClients {
		conn.WriteJSON(logEntry)
	}
	h.logClientsMu.Unlock()
}
