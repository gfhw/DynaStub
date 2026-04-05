package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HTTPTestStubSpec defines the desired state of HTTPTestStub
type HTTPTestStubSpec struct {
	Protocol string    `json:"protocol"`
	Request  Request   `json:"request"`
	Response Response  `json:"response"`
	Scripts  []Script  `json:"scripts,omitempty"`
	TLS      TLSConfig `json:"tls,omitempty"`
	Stubs    []Stub    `json:"stubs,omitempty"`
}

// Request 请求匹配规则
type Request struct {
	Method  string   `json:"method"`
	URL     URL      `json:"url"`
	Body    *Body    `json:"body,omitempty"`    // 请求体匹配
	Headers []Header `json:"headers,omitempty"` // 请求头匹配
}

// Body 请求体匹配规则
type Body struct {
	Type     string `json:"type"`               // json, xml, text, regex
	Matcher  string `json:"matcher"`            // equalTo, contains, matches, jsonPath
	Value    string `json:"value"`              // 匹配值
	JSONPath string `json:"jsonPath,omitempty"` // JSONPath 表达式（当 matcher 为 jsonPath 时使用）
}

// Header 请求头匹配规则
type Header struct {
	Name    string `json:"name"`
	Matcher string `json:"matcher"` // equalTo, contains, matches
	Value   string `json:"value"`
}

// URL URL匹配规则
type URL struct {
	Type    string `json:"type"`
	Pattern string `json:"pattern,omitempty"`
	Regex   string `json:"regex,omitempty"`
}

// Response 响应配置
type Response struct {
	Type   string  `json:"type"`
	Static *Static `json:"static,omitempty"`
	Script *Script `json:"script,omitempty"`
	Proxy  *Proxy  `json:"proxy,omitempty"` // 代理转发配置
}

// Proxy 代理转发配置
type Proxy struct {
	Target    string          `json:"target"`              // 目标地址，如 http://real-service:8080
	Record    bool            `json:"record,omitempty"`    // 是否录制响应
	Transform *ProxyTransform `json:"transform,omitempty"` // 请求/响应转换
}

// ProxyTransform 代理转发时的转换配置
type ProxyTransform struct {
	RequestHeaders  map[string]string `json:"requestHeaders,omitempty"`  // 添加/修改请求头
	ResponseHeaders map[string]string `json:"responseHeaders,omitempty"` // 添加/修改响应头
}

// Static 静态响应配置
type Static struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	Delay   *Delay            `json:"delay,omitempty"` // 延迟配置
}

// Delay 延迟配置
type Delay struct {
	Fixed  int          `json:"fixed,omitempty"`  // 固定延迟（毫秒）
	Random *RandomDelay `json:"random,omitempty"` // 随机延迟
}

// RandomDelay 随机延迟配置
type RandomDelay struct {
	Min int `json:"min"` // 最小延迟（毫秒）
	Max int `json:"max"` // 最大延迟（毫秒）
}

// Script 脚本配置
type Script struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"`              // shell, python, etc.
	Path    string            `json:"path"`              // 脚本文件路径
	Timeout int               `json:"timeout"`           // 超时时间（秒）
	Input   []string          `json:"input"`             // 脚本输入
	Content string            `json:"content,omitempty"` // 内联脚本内容
	Env     map[string]string `json:"env,omitempty"`     // 环境变量
}

// TLSConfig TLS配置
type TLSConfig struct {
	Enabled    bool   `json:"enabled"`
	SecretName string `json:"secretName"`
}

// Stub 单个stub配置
type Stub struct {
	Request       Request        `json:"request"`
	ResponseRules []ResponseRule `json:"responseRules"`
	Counter       Counter        `json:"counter"`
}

// ResponseRule 响应规则
type ResponseRule struct {
	Rule     Rule    `json:"rule"`
	Response *Static `json:"response,omitempty"`
}

// Rule 规则条件
type Rule struct {
	Type  string `json:"type"`
	Start int    `json:"start,omitempty"`
	End   int    `json:"end,omitempty"`
}

// Counter 计数器配置
type Counter struct {
	Reset      bool `json:"reset"`
	ResetAfter int  `json:"resetAfter"`
}

// HTTPTestStubStatus defines the observed state of HTTPTestStub
type HTTPTestStubStatus struct {
	// Phase 表示当前状态阶段：Pending, Running, Failed
	Phase string `json:"phase,omitempty"`
	// RequestCount 当前请求计数（用于计数器规则）
	RequestCount int `json:"requestCount,omitempty"`
	// TotalRequests 总请求数（累计）
	TotalRequests int64 `json:"totalRequests,omitempty"`
	// LastRequestTime 最后请求时间
	LastRequestTime *metav1.Time `json:"lastRequestTime,omitempty"`
	// AvgResponseTime 平均响应时间（毫秒）
	AvgResponseTime int64 `json:"avgResponseTime,omitempty"`
	// ErrorRate 错误率（百分比，0-100）
	ErrorRate int `json:"errorRate,omitempty"`
	// TotalErrors 总错误数
	TotalErrors int64 `json:"totalErrors,omitempty"`
	// ActiveConnections 当前活跃连接数
	ActiveConnections int32 `json:"activeConnections,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// HTTPTestStub is the Schema for the httpteststubs API
type HTTPTestStub struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HTTPTestStubSpec   `json:"spec,omitempty"`
	Status HTTPTestStubStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// HTTPTestStubList contains a list of HTTPTestStub
type HTTPTestStubList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HTTPTestStub `json:"items"`
}
