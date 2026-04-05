package controller

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"regexp"
	"runtime"
	"strings"
	"time"

	beego "github.com/beego/beego/v2/server/web"
	httpteststubv1 "httpteststub.example.com/api/v1"
)

type StubController struct {
	beego.Controller
}

func (c *StubController) Get() {
	c.handleRequest("GET")
}

func (c *StubController) Post() {
	c.handleRequest("POST")
}

func (c *StubController) Put() {
	c.handleRequest("PUT")
}

func (c *StubController) Delete() {
	c.handleRequest("DELETE")
}

func (c *StubController) Patch() {
	c.handleRequest("PATCH")
}

func (c *StubController) Head() {
	c.handleRequest("HEAD")
}

func (c *StubController) Options() {
	c.handleRequest("OPTIONS")
}

// GetProtocol 获取当前请求的协议类型
func (c *StubController) GetProtocol() string {
	// 从请求中获取协议类型
	scheme := c.Ctx.Input.Scheme()
	if scheme == "https" {
		return "https"
	}
	return "http"
}

func (c *StubController) handleRequest(method string) {
	startTime := time.Now()
	path := c.Ctx.Input.URL()
	protocol := c.GetProtocol()

	stub := GetMatchingStub(method, path, protocol)
	if stub == nil {
		c.Ctx.Output.SetStatus(http.StatusNotFound)
		c.Ctx.Output.JSON(map[string]string{
			"error":    "No matching stub found",
			"method":   method,
			"path":     path,
			"protocol": protocol,
		}, false, false)
		return
	}

	stubKey := fmt.Sprintf("%s/%s/%s", stub.Namespace, stub.Name, protocol)

	if !CheckResourceLimit(stubKey, 1000) {
		duration := time.Since(startTime)
		RecordRequestStats(stubKey, duration, true)
		c.Ctx.Output.SetStatus(http.StatusTooManyRequests)
		c.Ctx.Output.JSON(map[string]string{
			"error": "Rate limit exceeded",
		}, false, false)
		return
	}

	c.handleStubResponse(stub, method, path, startTime, protocol)
}

func (c *StubController) handleStubResponse(stub *httpteststubv1.HTTPTestStub, method, path string, startTime time.Time, protocol string) {
	stubKey := fmt.Sprintf("%s/%s/%s", stub.Namespace, stub.Name, protocol)

	// 增加活跃连接数
	IncrementActiveConnections(stubKey)
	defer DecrementActiveConnections(stubKey)

	isError := true
	defer func() {
		duration := time.Since(startTime)
		RecordRequestStats(stubKey, duration, isError)
		// 实时更新 CR status
		UpdateStubStatusRealtime(stub.Namespace, stub.Name)
	}()

	for _, s := range stub.Spec.Stubs {
		if matchRequest(method, path, &s.Request) {
			err := c.handleStubRules(stubKey, &s)
			if !err {
				isError = false
			}
			return
		}
	}

	if matchRequest(method, path, &stub.Spec.Request) {
		err := c.handleResponse(&stub.Spec.Response)
		if !err {
			isError = false
		}
		return
	}

	c.Ctx.Output.SetStatus(http.StatusNotFound)
	c.Ctx.Output.JSON(map[string]string{
		"error": "No matching rule found",
	}, false, false)
}

func (c *StubController) handleStubRules(stubKey string, stub *httpteststubv1.Stub) bool {
	count := IncrementCounter(stubKey)
	log.Printf("[DEBUG] handleStubRules: stubKey=%s, count=%d", stubKey, count)

	if stub.Counter.ResetAfter > 0 {
		ResetCounter(stubKey, stub.Counter.ResetAfter)
		log.Printf("[DEBUG] ResetCounter called: stubKey=%s, resetAfter=%d", stubKey, stub.Counter.ResetAfter)
	}

	// 先检查 range 规则
	for i, rule := range stub.ResponseRules {
		log.Printf("[DEBUG] Checking range rule %d: type=%s, start=%d, end=%d, count=%d",
			i, rule.Rule.Type, rule.Rule.Start, rule.Rule.End, count)
		if rule.Rule.Type == "range" && count >= rule.Rule.Start && count <= rule.Rule.End {
			log.Printf("[DEBUG] Range rule %d matched! Returning status %d", i, rule.Response.Status)
			if rule.Response != nil {
				c.sendStaticResponse(rule.Response)
			}
			return false // 成功，不是错误
		}
	}

	// 再检查 default 规则（兜底规则）
	for i, rule := range stub.ResponseRules {
		log.Printf("[DEBUG] Checking default rule %d: type=%s", i, rule.Rule.Type)
		if rule.Rule.Type == "default" {
			log.Printf("[DEBUG] Default rule %d matched! Returning status %d", i, rule.Response.Status)
			if rule.Response != nil {
				c.sendStaticResponse(rule.Response)
			}
			return false // 成功，不是错误
		}
	}

	log.Printf("[DEBUG] No rules matched, returning default OK response")
	c.Ctx.Output.SetStatus(http.StatusOK)
	c.Ctx.Output.JSON(map[string]interface{}{
		"result": true,
		"count":  count,
	}, false, false)
	return false // 成功，不是错误
}

func (c *StubController) handleResponse(response *httpteststubv1.Response) bool {
	switch response.Type {
	case "static":
		if response.Static != nil {
			c.sendStaticResponse(response.Static)
			return false // 成功
		}
		return true // 错误：静态响应为空
	case "script":
		if response.Script != nil {
			return c.sendScriptResponse(response.Script)
		}
		return true // 错误：脚本响应为空
	case "proxy":
		if response.Proxy != nil {
			return c.sendProxyResponse(response.Proxy)
		}
		return true // 错误：代理配置为空
	default:
		c.Ctx.Output.SetStatus(http.StatusInternalServerError)
		c.Ctx.Output.JSON(map[string]string{
			"error": "Unknown response type",
		}, false, false)
		return true // 错误：未知响应类型
	}
	return true // 默认错误
}

func (c *StubController) sendStaticResponse(static *httpteststubv1.Static) {
	// 应用延迟
	applyDelay(static.Delay)

	for key, value := range static.Headers {
		c.Ctx.Output.Header(key, value)
	}

	c.Ctx.Output.SetStatus(static.Status)
	c.Ctx.Output.Body([]byte(static.Body))
}

func (c *StubController) sendScriptResponse(script *httpteststubv1.Script) bool {
	executor := NewScriptExecutor()
	requestContext := map[string]interface{}{
		"method":  c.Ctx.Request.Method,
		"path":    c.Ctx.Request.URL.Path,
		"headers": c.Ctx.Request.Header,
		"body":    "",
	}

	statusCode, _, body, err := executor.Execute(script, requestContext)
	if err != nil {
		c.Ctx.Output.SetStatus(statusCode)
		c.Ctx.Output.JSON(map[string]interface{}{
			"error": fmt.Sprintf("Script execution failed: %v", err),
		}, false, false)
		return true // 错误
	}

	_, scriptHeaders, scriptStatus := ParseScriptOutput(body)
	if scriptStatus != 200 {
		statusCode = scriptStatus
	}

	for key, value := range scriptHeaders {
		c.Ctx.Output.Header(key, value)
	}

	c.Ctx.Output.SetStatus(statusCode)
	c.Ctx.Output.Body(body)
	return statusCode >= 400 // 4xx/5xx 认为是错误
}

func (c *StubController) sendProxyResponse(proxy *httpteststubv1.Proxy) bool {
	// 创建代理请求
	targetURL := proxy.Target + c.Ctx.Request.URL.RequestURI()
	req, err := http.NewRequest(c.Ctx.Request.Method, targetURL, c.Ctx.Request.Body)
	if err != nil {
		c.Ctx.Output.SetStatus(http.StatusInternalServerError)
		c.Ctx.Output.JSON(map[string]string{
			"error": fmt.Sprintf("Failed to create proxy request: %v", err),
		}, false, false)
		return true // 错误
	}

	// 复制请求头
	for key, values := range c.Ctx.Request.Header {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	// 应用请求头转换
	if proxy.Transform != nil {
		for key, value := range proxy.Transform.RequestHeaders {
			req.Header.Set(key, value)
		}
	}

	// 发送请求到目标服务器
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		c.Ctx.Output.SetStatus(http.StatusBadGateway)
		c.Ctx.Output.JSON(map[string]string{
			"error": fmt.Sprintf("Failed to forward request: %v", err),
		}, false, false)
		return true // 错误
	}
	defer resp.Body.Close()

	// 复制响应头
	for key, values := range resp.Header {
		for _, value := range values {
			c.Ctx.Output.Header(key, value)
		}
	}

	// 应用响应头转换
	if proxy.Transform != nil {
		for key, value := range proxy.Transform.ResponseHeaders {
			c.Ctx.Output.Header(key, value)
		}
	}

	// 复制响应状态码和体
	c.Ctx.Output.SetStatus(resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.Ctx.Output.SetStatus(http.StatusInternalServerError)
		c.Ctx.Output.JSON(map[string]string{
			"error": fmt.Sprintf("Failed to read response body: %v", err),
		}, false, false)
		return true // 错误
	}

	c.Ctx.Output.Body(body)
	return resp.StatusCode >= 400 // 4xx/5xx 认为是错误
}

type HealthController struct {
	beego.Controller
}

func (c *HealthController) Get() {
	stubs := GetAllStubs()

	stubStats := make([]map[string]interface{}, 0)
	for _, stub := range stubs {
		stubKey := fmt.Sprintf("%s/%s", stub.Namespace, stub.Name)
		requestsPerMinute, totalRequests, uptime := GetResourceStats(stubKey)

		stats := map[string]interface{}{
			"name":                stub.Name,
			"namespace":           stub.Namespace,
			"phase":               stub.Status.Phase,
			"requests_per_minute": requestsPerMinute,
			"total_requests":      totalRequests,
			"uptime_seconds":      int64(uptime.Seconds()),
		}
		stubStats = append(stubStats, stats)
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	c.Ctx.Output.JSON(map[string]interface{}{
		"status":      "healthy",
		"timestamp":   time.Now().Unix(),
		"stubs_count": len(stubs),
		"stubs":       stubStats,
		"system": map[string]interface{}{
			"goroutines":      runtime.NumGoroutine(),
			"memory_alloc_mb": memStats.Alloc / 1024 / 1024,
			"memory_sys_mb":   memStats.Sys / 1024 / 1024,
			"gc_cycles":       memStats.NumGC,
		},
	}, false, false)
}

type ReadyController struct {
	beego.Controller
}

func (c *ReadyController) Get() {
	c.Ctx.Output.JSON(map[string]string{
		"status": "ready",
	}, false, false)
}

// matchBody 匹配请求体
func matchBody(requestBody string, bodyConfig *httpteststubv1.Body) bool {
	if bodyConfig == nil {
		return true
	}

	switch bodyConfig.Matcher {
	case "equalTo":
		return requestBody == bodyConfig.Value
	case "contains":
		return strings.Contains(requestBody, bodyConfig.Value)
	case "matches":
		matched, _ := regexp.MatchString(bodyConfig.Value, requestBody)
		return matched
	case "jsonPath":
		// 简单实现：检查 JSON 中是否包含指定路径的值
		// 实际项目中可以使用 github.com/tidwall/gjson
		return strings.Contains(requestBody, bodyConfig.JSONPath)
	default:
		return strings.Contains(requestBody, bodyConfig.Value)
	}
}

// matchHeaders 匹配请求头
func matchHeaders(requestHeaders map[string]string, headerConfigs []httpteststubv1.Header) bool {
	if len(headerConfigs) == 0 {
		return true
	}

	for _, header := range headerConfigs {
		value, exists := requestHeaders[header.Name]
		if !exists {
			return false
		}

		switch header.Matcher {
		case "equalTo":
			if value != header.Value {
				return false
			}
		case "contains":
			if !strings.Contains(value, header.Value) {
				return false
			}
		case "matches":
			matched, _ := regexp.MatchString(header.Value, value)
			if !matched {
				return false
			}
		default:
			if !strings.Contains(value, header.Value) {
				return false
			}
		}
	}
	return true
}

// applyDelay 应用延迟
func applyDelay(delayConfig *httpteststubv1.Delay) {
	if delayConfig == nil {
		return
	}

	if delayConfig.Fixed > 0 {
		time.Sleep(time.Duration(delayConfig.Fixed) * time.Millisecond)
	} else if delayConfig.Random != nil {
		min := delayConfig.Random.Min
		max := delayConfig.Random.Max
		if min < max {
			delay := min + rand.Intn(max-min)
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
	}
}
