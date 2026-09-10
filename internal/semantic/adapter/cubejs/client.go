package cubejs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client Cube.js REST API 客户端。
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// NewClient 创建 Cube.js 客户端。
func NewClient(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ─── 响应类型 ────────────────────────────────────────────────────────────────

// MetaResponse /v1/meta 响应。
type MetaResponse struct {
	Cubes []Cube `json:"cubes"`
}

// Cube Cube.js Cube 元数据。
type Cube struct {
	Name        string   `json:"name"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	DataSource  string   `json:"dataSource"`
	Measures    []Member `json:"measures"`
	Dimensions  []Member `json:"dimensions"`
	Joins       []Join   `json:"joins"`
	Segments    []Member `json:"segments"`
}

// Member Cube.js 度量/维度成员。
type Member struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Type        string `json:"type"`
	SQL         string `json:"sql,omitempty"`
	PrimaryKey  bool   `json:"primaryKey,omitempty"`
}

// Join Cube.js JOIN 定义。
type Join struct {
	Name         string `json:"name"`
	Relationship string `json:"relationship"`
	JoinType     string `json:"joinType"`
	SQL          string `json:"sql"`
}

// ─── 查询类型 ────────────────────────────────────────────────────────────────

// Query Cube.js 查询格式。
type Query struct {
	Measures       []string        `json:"measures,omitempty"`
	Dimensions     []string        `json:"dimensions,omitempty"`
	TimeDimensions []TimeDimension `json:"timeDimensions,omitempty"`
	Filters        []Filter        `json:"filters,omitempty"`
	Order          [][]string      `json:"order,omitempty"`
	Limit          int             `json:"limit,omitempty"`
	Offset         int             `json:"offset,omitempty"`
}

// TimeDimension 时间维度查询。
type TimeDimension struct {
	Dimension   string   `json:"dimension"`
	DateRange   []string `json:"dateRange,omitempty"`
	Granularity string   `json:"granularity,omitempty"`
}

// Filter 过滤条件。
type Filter struct {
	Member   string   `json:"member"`
	Operator string   `json:"operator"`
	Values   []string `json:"values,omitempty"`
}

// ─── Load 响应类型 ────────────────────────────────────────────────────────────

// LoadResponse /v1/load 响应。
type LoadResponse struct {
	Annotation Annotation                 `json:"annotation"`
	Data       []map[string]interface{} `json:"data"`
}

// Annotation 查询结果注解。
type Annotation struct {
	Measures       map[string]MemberAnnotation `json:"measures"`
	Dimensions     map[string]MemberAnnotation `json:"dimensions"`
	TimeDimensions map[string]MemberAnnotation `json:"timeDimensions"`
}

// MemberAnnotation 成员注解。
type MemberAnnotation struct {
	Title string `json:"title"`
	Type  string `json:"type"`
}

// ─── API 方法 ────────────────────────────────────────────────────────────────

// Meta 获取 Cube 元数据。
func (c *Client) Meta(ctx context.Context) (*MetaResponse, error) {
	var resp MetaResponse
	if err := c.do(ctx, http.MethodGet, "/v1/meta", nil, &resp); err != nil {
		return nil, fmt.Errorf("cubejs meta: %w", err)
	}
	return &resp, nil
}

// Load 执行查询。
func (c *Client) Load(ctx context.Context, query *Query) (*LoadResponse, error) {
	var resp LoadResponse
	if err := c.do(ctx, http.MethodPost, "/v1/load", query, &resp); err != nil {
		return nil, fmt.Errorf("cubejs load: %w", err)
	}
	return &resp, nil
}

// Ping 探测连接。
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.Meta(ctx)
	return err
}

// ─── 内部方法 ────────────────────────────────────────────────────────────────

func (c *Client) do(ctx context.Context, method, path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if httpResp.StatusCode < 200 || httpResp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", httpResp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}
	}
	return nil
}
