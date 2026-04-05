package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	httpteststubv1 "httpteststub.example.com/api/v1"
)

var stubCache sync.Map
var stubCounters sync.Map
var resourceLimits sync.Map

type HTTPTestStubReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

type StubCounter struct {
	mu      sync.Mutex
	count   int
	resetAt int
}

type ResourceLimit struct {
	mu                   sync.Mutex
	requestCount         int
	lastResetTime        time.Time
	maxRequestsPerMinute int
	totalRequests        int
	startTime            time.Time
}

// StubStats 用于跟踪 stub 的请求统计信息
type StubStats struct {
	mu              sync.Mutex
	totalRequests   int64
	totalErrors     int64
	lastRequestTime time.Time
	totalDuration   time.Duration
}

// stubStatsMap 存储每个 stub 的统计信息
var stubStatsMap sync.Map

// globalClient 全局 Kubernetes 客户端，用于实时更新 status
var globalClient client.Client
var clientOnce sync.Once

// stubSpecHashMap 存储每个 stub 的 spec hash，用于检测变更
var stubSpecHashMap sync.Map

func (r *HTTPTestStubReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// 初始化全局客户端
	clientOnce.Do(func() {
		globalClient = r.Client
	})

	var httpTestStub httpteststubv1.HTTPTestStub
	if err := r.Get(ctx, req.NamespacedName, &httpTestStub); err != nil {
		log.Error(err, "unable to fetch HTTPTestStub")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	key := fmt.Sprintf("%s/%s", httpTestStub.Namespace, httpTestStub.Name)

	// 检查是否是新创建的 CR（不存在于缓存中）
	_, existsInCache := stubCache.Load(key)
	log.Info("[DEBUG] Reconcile: checking cache", "key", key, "existsInCache", existsInCache)

	stubCache.Store(key, &httpTestStub)

	if _, exists := stubCounters.Load(key); !exists {
		stubCounters.Store(key, &StubCounter{})
	}

	if _, exists := resourceLimits.Load(key); !exists {
		resourceLimits.Store(key, &ResourceLimit{
			lastResetTime: time.Now(),
		})
	}

	// 检测 spec 是否变更，如果变更则重置统计信息
	currentHash := calculateSpecHash(&httpTestStub.Spec)
	log.Info("[DEBUG] Reconcile: checking spec hash", "key", key, "currentHash", currentHash)

	if lastHash, exists := stubSpecHashMap.Load(key); exists {
		log.Info("[DEBUG] Reconcile: lastHash exists", "key", key, "lastHash", lastHash, "currentHash", currentHash)
		if lastHash.(string) != currentHash {
			// spec 发生变更，重置统计信息
			log.Info("HTTPTestStub spec changed, resetting stats", "name", httpTestStub.Name, "namespace", httpTestStub.Namespace)
			ResetStubStats(key)
		}
	} else if !existsInCache {
		// 新创建的 CR，重置计数器
		log.Info("HTTPTestStub created, resetting counter", "name", httpTestStub.Name, "namespace", httpTestStub.Namespace)
		ResetStubStats(key)
	}
	stubSpecHashMap.Store(key, currentHash)

	// 更新状态为 Running 并包含统计信息
	if err := r.UpdateStubStatus(ctx, &httpTestStub); err != nil {
		log.Error(err, "unable to update HTTPTestStub status")
		return ctrl.Result{}, err
	}

	log.Info("HTTPTestStub loaded into cache", "name", httpTestStub.Name, "namespace", httpTestStub.Namespace)

	return ctrl.Result{}, nil
}

func (r *HTTPTestStubReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := ctrl.NewControllerManagedBy(mgr).
		For(&httpteststubv1.HTTPTestStub{}).
		Named("httpteststub").
		Complete(r); err != nil {
		return err
	}

	// 启动定时器，每 10 秒更新一次所有 stub 的 status
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		ctx := context.Background()

		for range ticker.C {
			r.updateAllStubsStatus(ctx)
		}
	}()

	return nil
}

// updateAllStubsStatus 更新所有 stub 的 status
func (r *HTTPTestStubReconciler) updateAllStubsStatus(ctx context.Context) {
	stubCache.Range(func(key, value interface{}) bool {
		stub := value.(*httpteststubv1.HTTPTestStub)

		var currentStub httpteststubv1.HTTPTestStub
		if err := r.Get(ctx, client.ObjectKey{
			Namespace: stub.Namespace,
			Name:      stub.Name,
		}, &currentStub); err != nil {
			return true // 跳过获取失败的 stub
		}

		if err := r.UpdateStubStatus(ctx, &currentStub); err != nil {
			logf.Log.Error(err, "unable to update stub status", "name", stub.Name, "namespace", stub.Namespace)
		}

		return true
	})
}

func GetMatchingStub(method, path, protocol string) *httpteststubv1.HTTPTestStub {
	var matchedStub *httpteststubv1.HTTPTestStub

	stubCache.Range(func(key, value interface{}) bool {
		stub := value.(*httpteststubv1.HTTPTestStub)

		// 检查 protocol 是否匹配
		// 如果 CR 的 protocol 为空或 "both"，则匹配所有协议
		// 否则必须完全匹配
		stubProtocol := stub.Spec.Protocol
		if stubProtocol != "" && stubProtocol != "both" && stubProtocol != protocol {
			return true // 跳过不匹配的 protocol
		}

		for _, s := range stub.Spec.Stubs {
			if matchRequest(method, path, &s.Request) {
				matchedStub = stub
				return false
			}
		}

		if matchRequest(method, path, &stub.Spec.Request) {
			matchedStub = stub
			return false
		}

		return true
	})

	return matchedStub
}

func matchRequest(method, path string, request *httpteststubv1.Request) bool {
	if method != request.Method {
		return false
	}

	switch request.URL.Type {
	case "exact":
		return path == request.URL.Pattern
	case "pattern":
		return matchPattern(path, request.URL.Pattern)
	case "regex":
		return matchRegex(path, request.URL.Regex)
	default:
		return false
	}
}

func matchPattern(url, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if pattern == url {
		return true
	}

	// 处理多个通配符的情况
	patternParts := strings.Split(pattern, "*")
	if len(patternParts) == 1 {
		// 没有通配符，直接比较
		return url == pattern
	}

	// 检查前缀
	if patternParts[0] != "" && !strings.HasPrefix(url, patternParts[0]) {
		return false
	}

	// 检查后缀
	if patternParts[len(patternParts)-1] != "" && !strings.HasSuffix(url, patternParts[len(patternParts)-1]) {
		return false
	}

	// 检查中间部分
	currentIndex := len(patternParts[0])
	for i := 1; i < len(patternParts)-1; i++ {
		part := patternParts[i]
		if part == "" {
			continue
		}
		index := strings.Index(url[currentIndex:], part)
		if index == -1 {
			return false
		}
		currentIndex += index + len(part)
	}

	return true
}

func matchRegex(url, regex string) bool {
	matched, err := regexp.MatchString(regex, url)
	if err != nil {
		return false
	}
	return matched
}

func IncrementCounter(stubKey string) int {
	// 确保计数器存在
	counter, _ := stubCounters.LoadOrStore(stubKey, &StubCounter{})
	c := counter.(*StubCounter)
	c.mu.Lock()
	defer c.mu.Unlock()

	c.count++
	logf.Log.Info("[DEBUG] IncrementCounter", "stubKey", stubKey, "count", c.count)
	return c.count
}

func ResetCounter(stubKey string, resetAfter int) {
	if counter, ok := stubCounters.Load(stubKey); ok {
		c := counter.(*StubCounter)
		c.mu.Lock()
		defer c.mu.Unlock()

		if c.count >= resetAfter {
			c.count = 0
		}
	}
}

func CheckResourceLimit(stubKey string, maxRequestsPerMinute int) bool {
	if limit, ok := resourceLimits.Load(stubKey); ok {
		rl := limit.(*ResourceLimit)
		rl.mu.Lock()
		defer rl.mu.Unlock()

		now := time.Now()
		if now.Sub(rl.lastResetTime) > time.Minute {
			rl.requestCount = 0
			rl.lastResetTime = now
		}

		if rl.maxRequestsPerMinute == 0 {
			rl.maxRequestsPerMinute = maxRequestsPerMinute
		}

		if rl.startTime.IsZero() {
			rl.startTime = now
		}

		if rl.requestCount >= rl.maxRequestsPerMinute {
			return false
		}

		rl.requestCount++
		rl.totalRequests++
		return true
	}
	return true
}

func GetResourceStats(stubKey string) (requestsPerMinute, totalRequests int, uptime time.Duration) {
	if limit, ok := resourceLimits.Load(stubKey); ok {
		rl := limit.(*ResourceLimit)
		rl.mu.Lock()
		defer rl.mu.Unlock()

		requestsPerMinute = rl.requestCount
		totalRequests = rl.totalRequests
		if !rl.startTime.IsZero() {
			uptime = time.Since(rl.startTime)
		}
	}
	return
}

func SerializeResponse(response interface{}) ([]byte, error) {
	return json.Marshal(response)
}

func GetStubFromCache(key string) *httpteststubv1.HTTPTestStub {
	if value, ok := stubCache.Load(key); ok {
		return value.(*httpteststubv1.HTTPTestStub)
	}
	return nil
}

func GetAllStubs() []*httpteststubv1.HTTPTestStub {
	var stubs []*httpteststubv1.HTTPTestStub

	stubCache.Range(func(key, value interface{}) bool {
		stubs = append(stubs, value.(*httpteststubv1.HTTPTestStub))
		return true
	})

	return stubs
}

func RemoveStubFromCache(key string) {
	stubCache.Delete(key)
	stubCounters.Delete(key)
	resourceLimits.Delete(key)
	stubStatsMap.Delete(key)
}

// RecordRequestStats 记录请求统计信息
func RecordRequestStats(stubKey string, duration time.Duration, isError bool) {
	stats, exists := stubStatsMap.Load(stubKey)
	if !exists {
		stats = &StubStats{}
		stubStatsMap.Store(stubKey, stats)
	}

	s := stats.(*StubStats)
	s.mu.Lock()
	defer s.mu.Unlock()

	s.totalRequests++
	s.lastRequestTime = time.Now()
	s.totalDuration += duration
	if isError {
		s.totalErrors++
	}
}

// GetStubStats 获取 stub 的统计信息
func GetStubStats(stubKey string) (totalRequests, totalErrors int64, lastRequestTime time.Time, avgResponseTime int64, errorRate int) {
	stats, exists := stubStatsMap.Load(stubKey)
	if !exists {
		return 0, 0, time.Time{}, 0, 0
	}

	s := stats.(*StubStats)
	s.mu.Lock()
	defer s.mu.Unlock()

	totalRequests = s.totalRequests
	totalErrors = s.totalErrors
	lastRequestTime = s.lastRequestTime

	if s.totalRequests > 0 {
		avgResponseTime = int64(s.totalDuration.Milliseconds() / int64(s.totalRequests))
		errorRate = int(float64(s.totalErrors) * 100 / float64(s.totalRequests))
	}

	return
}

// UpdateStubStatus 更新 stub 的 status 到 CR
func (r *HTTPTestStubReconciler) UpdateStubStatus(ctx context.Context, stub *httpteststubv1.HTTPTestStub) error {
	key := fmt.Sprintf("%s/%s", stub.Namespace, stub.Name)

	// 获取 HTTP 和 HTTPS 的统计信息并合并
	httpTotalRequests, httpTotalErrors, httpLastRequestTime, httpAvgResponseTime, _ := GetStubStats(key + "/http")
	httpsTotalRequests, httpsTotalErrors, httpsLastRequestTime, httpsAvgResponseTime, _ := GetStubStats(key + "/https")

	// 合并统计信息
	totalRequests := httpTotalRequests + httpsTotalRequests
	totalErrors := httpTotalErrors + httpsTotalErrors

	// 选择最近的请求时间
	var lastRequestTime time.Time
	if !httpLastRequestTime.IsZero() && !httpsLastRequestTime.IsZero() {
		if httpLastRequestTime.After(httpsLastRequestTime) {
			lastRequestTime = httpLastRequestTime
		} else {
			lastRequestTime = httpsLastRequestTime
		}
	} else if !httpLastRequestTime.IsZero() {
		lastRequestTime = httpLastRequestTime
	} else if !httpsLastRequestTime.IsZero() {
		lastRequestTime = httpsLastRequestTime
	}

	// 计算加权平均响应时间
	var avgResponseTime int64
	if totalRequests > 0 {
		totalDuration := httpTotalRequests*httpAvgResponseTime + httpsTotalRequests*httpsAvgResponseTime
		avgResponseTime = totalDuration / totalRequests
	}

	// 计算总错误率
	var errorRate int
	if totalRequests > 0 {
		errorRate = int(float64(totalErrors) * 100 / float64(totalRequests))
	}

	stub.Status.Phase = "Running"
	stub.Status.TotalRequests = totalRequests
	stub.Status.TotalErrors = totalErrors
	stub.Status.AvgResponseTime = avgResponseTime
	stub.Status.ErrorRate = errorRate

	if !lastRequestTime.IsZero() {
		t := metav1.NewTime(lastRequestTime)
		stub.Status.LastRequestTime = &t
	}

	return r.Status().Update(ctx, stub)
}

// activeConnectionsMap 跟踪每个 stub 的活跃连接数
var activeConnectionsMap sync.Map

// IncrementActiveConnections 增加活跃连接数
func IncrementActiveConnections(stubKey string) {
	count, _ := activeConnectionsMap.LoadOrStore(stubKey, int32(0))
	activeConnectionsMap.Store(stubKey, count.(int32)+1)
}

// DecrementActiveConnections 减少活跃连接数
func DecrementActiveConnections(stubKey string) {
	count, exists := activeConnectionsMap.Load(stubKey)
	if exists {
		c := count.(int32)
		if c > 0 {
			activeConnectionsMap.Store(stubKey, c-1)
		}
	}
}

// GetActiveConnections 获取活跃连接数
func GetActiveConnections(stubKey string) int32 {
	count, exists := activeConnectionsMap.Load(stubKey)
	if !exists {
		return 0
	}
	return count.(int32)
}

// UpdateStubStatusRealtime 实时更新 stub 的 status 到 CR（非阻塞）
func UpdateStubStatusRealtime(namespace, name string) {
	if globalClient == nil {
		logf.Log.WithName("status-update").Info("globalClient is nil, skipping status update")
		return // 客户端未初始化，跳过
	}

	key := fmt.Sprintf("%s/%s", namespace, name)

	// 获取 HTTP 和 HTTPS 的统计信息并合并
	httpTotalRequests, httpTotalErrors, httpLastRequestTime, httpAvgResponseTime, _ := GetStubStats(key + "/http")
	httpsTotalRequests, httpsTotalErrors, httpsLastRequestTime, httpsAvgResponseTime, _ := GetStubStats(key + "/https")

	// 获取活跃连接数
	httpActiveConns := GetActiveConnections(key + "/http")
	httpsActiveConns := GetActiveConnections(key + "/https")

	// 合并统计信息
	totalRequests := httpTotalRequests + httpsTotalRequests
	totalErrors := httpTotalErrors + httpsTotalErrors
	activeConnections := httpActiveConns + httpsActiveConns

	// 选择最近的请求时间
	var lastRequestTime time.Time
	if !httpLastRequestTime.IsZero() && !httpsLastRequestTime.IsZero() {
		if httpLastRequestTime.After(httpsLastRequestTime) {
			lastRequestTime = httpLastRequestTime
		} else {
			lastRequestTime = httpsLastRequestTime
		}
	} else if !httpLastRequestTime.IsZero() {
		lastRequestTime = httpLastRequestTime
	} else if !httpsLastRequestTime.IsZero() {
		lastRequestTime = httpsLastRequestTime
	}

	// 计算加权平均响应时间
	var avgResponseTime int64
	if totalRequests > 0 {
		totalDuration := httpTotalRequests*httpAvgResponseTime + httpsTotalRequests*httpsAvgResponseTime
		avgResponseTime = totalDuration / totalRequests
	}

	// 计算总错误率
	var errorRate int
	if totalRequests > 0 {
		errorRate = int(float64(totalErrors) * 100 / float64(totalRequests))
	}

	logger := logf.Log.WithName("status-update")

	// 使用后台 goroutine 更新 status，避免阻塞请求处理
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		var stub httpteststubv1.HTTPTestStub
		err := globalClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &stub)
		if err != nil {
			logger.Error(err, "failed to get stub for status update", "namespace", namespace, "name", name)
			return
		}

		stub.Status.Phase = "Running"
		stub.Status.TotalRequests = totalRequests
		stub.Status.TotalErrors = totalErrors
		stub.Status.AvgResponseTime = avgResponseTime
		stub.Status.ErrorRate = errorRate
		stub.Status.ActiveConnections = activeConnections

		if !lastRequestTime.IsZero() {
			t := metav1.NewTime(lastRequestTime)
			stub.Status.LastRequestTime = &t
		}

		// 获取计数器值作为 requestCount（优先使用 HTTP 计数器）
		httpCounterKey := key + "/http"
		httpsCounterKey := key + "/https"

		// 尝试获取 HTTP 计数器
		if counter, exists := stubCounters.Load(httpCounterKey); exists {
			c := counter.(*StubCounter)
			c.mu.Lock()
			stub.Status.RequestCount = c.count
			c.mu.Unlock()
		} else if counter, exists := stubCounters.Load(httpsCounterKey); exists {
			// 如果没有 HTTP 计数器，尝试 HTTPS 计数器
			c := counter.(*StubCounter)
			c.mu.Lock()
			stub.Status.RequestCount = c.count
			c.mu.Unlock()
		} else if counter, exists := stubCounters.Load(key); exists {
			// 最后尝试主计数器（兼容旧逻辑）
			c := counter.(*StubCounter)
			c.mu.Lock()
			stub.Status.RequestCount = c.count
			c.mu.Unlock()
		}

		err = globalClient.Status().Update(ctx, &stub)
		if err != nil {
			logger.Error(err, "failed to update stub status", "namespace", namespace, "name", name)
		} else {
			logger.Info("successfully updated stub status", "namespace", namespace, "name", name, "requests", totalRequests)
		}
	}()
}

// calculateSpecHash 计算 spec 的 hash 值
func calculateSpecHash(spec *httpteststubv1.HTTPTestStubSpec) string {
	// 使用 JSON 序列化后计算简单 hash
	data, _ := json.Marshal(spec)
	h := 0
	for i := 0; i < len(data); i++ {
		h = 31*h + int(data[i])
	}
	return fmt.Sprintf("%d", h)
}

// ResetStubStats 重置指定 stub 的所有统计信息
func ResetStubStats(key string) {
	logf.Log.Info("[DEBUG] ResetStubStats called", "key", key)

	// 重置 HTTP 统计
	httpKey := key + "/http"
	stubStatsMap.Delete(httpKey)
	activeConnectionsMap.Delete(httpKey)

	// 重置 HTTPS 统计
	httpsKey := key + "/https"
	stubStatsMap.Delete(httpsKey)
	activeConnectionsMap.Delete(httpsKey)

	// 重置计数器（键包含协议）
	httpCounterKey := key + "/http"
	httpsCounterKey := key + "/https"
	stubCounters.Delete(httpCounterKey)
	stubCounters.Delete(httpsCounterKey)

	logf.Log.Info("[DEBUG] ResetStubStats completed", "key", key, "httpCounterKey", httpCounterKey, "httpsCounterKey", httpsCounterKey)
}
