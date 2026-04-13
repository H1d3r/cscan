package onlineapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// QuakeClient Quake API客户端
type QuakeClient struct {
	apiKey string
	client *http.Client
}

// NewQuakeClient 创建Quake客户端
func NewQuakeClient(apiKey string) *QuakeClient {
	return &QuakeClient{
		apiKey: apiKey,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// QuakeCode 自定义类型处理 code 字段（可能是 int 或 string）
type QuakeCode struct {
	IntVal int
	StrVal string
	IsInt  bool
}

func (c *QuakeCode) UnmarshalJSON(data []byte) error {
	// 尝试解析为整数
	var intVal int
	if err := json.Unmarshal(data, &intVal); err == nil {
		c.IntVal = intVal
		c.IsInt = true
		return nil
	}
	// 尝试解析为字符串
	var strVal string
	if err := json.Unmarshal(data, &strVal); err == nil {
		c.StrVal = strVal
		c.IsInt = false
		// 尝试将字符串转为数字
		if intVal, err := strconv.Atoi(strVal); err == nil {
			c.IntVal = intVal
			c.IsInt = true
		}
		return nil
	}
	return nil
}

// IsSuccess 判断是否成功
func (c *QuakeCode) IsSuccess() bool {
	if c.IsInt {
		return c.IntVal == 0
	}
	return c.StrVal == "" || c.StrVal == "0"
}

// Error 获取错误信息
func (c *QuakeCode) Error() string {
	if c.IsInt {
		return fmt.Sprintf("%d", c.IntVal)
	}
	return c.StrVal
}

// QuakeResponse Quake响应
type QuakeResponse struct {
	Code    QuakeCode     `json:"code"`
	Message string        `json:"message"`
	Data    QuakeDataList `json:"data"`
	Meta    QuakeMeta     `json:"meta"`
}

// QuakeDataList 自定义类型处理 data 字段（可能是数组或对象）
// Quake API 在配额用尽或无数据时可能返回对象而非数组
type QuakeDataList struct {
	Items       []QuakeData
	IsExhausted bool // 标记是否配额用尽
}

func (d *QuakeDataList) UnmarshalJSON(data []byte) error {
	// 尝试解析为数组
	var arr []QuakeData
	if err := json.Unmarshal(data, &arr); err == nil {
		d.Items = arr
		d.IsExhausted = false
		return nil
	}
	// 如果是对象（配额用尽等情况），标记并返回空数组
	var obj map[string]interface{}
	if err := json.Unmarshal(data, &obj); err == nil {
		d.Items = []QuakeData{}
		d.IsExhausted = true // 标记配额用尽
		return nil
	}
	// 其他情况返回空数组
	d.Items = []QuakeData{}
	d.IsExhausted = false
	return nil
}

// QuakeMeta Quake元数据
type QuakeMeta struct {
	Pagination struct {
		Count     int `json:"count"`
		PageIndex int `json:"page_index"`
		PageSize  int `json:"page_size"`
		Total     int `json:"total"`
	} `json:"pagination"`
}

// QuakeASN 自定义类型处理 asn 字段（可能是对象或数字）
type QuakeASN struct {
	Number int    `json:"number"`
	Org    string `json:"org"`
}

func (a *QuakeASN) UnmarshalJSON(data []byte) error {
	// 尝试解析为对象
	type asnObj struct {
		Number int    `json:"number"`
		Org    string `json:"org"`
	}
	var obj asnObj
	if err := json.Unmarshal(data, &obj); err == nil {
		a.Number = obj.Number
		a.Org = obj.Org
		return nil
	}
	// 尝试解析为数字（ASN号码）
	var num int
	if err := json.Unmarshal(data, &num); err == nil {
		a.Number = num
		a.Org = ""
		return nil
	}
	// 尝试解析为字符串
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		a.Number = 0
		a.Org = str
		return nil
	}
	// 如果是 null 或其他，忽略
	a.Number = 0
	a.Org = ""
	return nil
}

// QuakeData Quake数据
type QuakeData struct {
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	Hostname string `json:"hostname"`
	Service  struct {
		Name     string `json:"name"`
		Product  string `json:"product"`
		Version  string `json:"version"`
		Response string `json:"response"`
		Cert     string `json:"cert"`
		HTTP     struct {
			Title      string `json:"title"`
			StatusCode int    `json:"status_code"`
			Server     string `json:"server"`
			Host       string `json:"host"`
			Path       string `json:"path"`
		} `json:"http"`
	} `json:"service"`
	Location struct {
		CountryCode string  `json:"country_code"`
		CountryCN   string  `json:"country_cn"`
		CountryEN   string  `json:"country_en"`
		ProvinceCN  string  `json:"province_cn"`
		ProvinceEN  string  `json:"province_en"`
		CityCN      string  `json:"city_cn"`
		CityEN      string  `json:"city_en"`
		DistrictCN  string  `json:"district_cn"`
		DistrictEN  string  `json:"district_en"`
		ISP         string  `json:"isp"`
		Lat         float64 `json:"lat"`
		Lon         float64 `json:"lon"`
	} `json:"location"`
	ASN        QuakeASN         `json:"asn"`
	Time       string           `json:"time"`
	Transport  string           `json:"transport"`
	Components []QuakeComponent `json:"components"`
}

// QuakeComponent 组件信息
type QuakeComponent struct {
	ProductLevel  StringOrArray `json:"product_level"`
	ProductType   StringOrArray `json:"product_type"`
	ProductVendor StringOrArray `json:"product_vendor"`
	ProductNameCN string        `json:"product_name_cn"`
	ProductNameEN string        `json:"product_name_en"`
	Version       string        `json:"version"`
}

// StringOrArray 自定义类型处理可能是字符串或字符串数组的字段
type StringOrArray []string

func (s *StringOrArray) UnmarshalJSON(data []byte) error {
	// 尝试解析为字符串数组
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*s = arr
		return nil
	}
	// 尝试解析为单个字符串
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		if str != "" {
			*s = []string{str}
		} else {
			*s = []string{}
		}
		return nil
	}
	// 如果是 null 或其他，返回空数组
	*s = []string{}
	return nil
}

// Search 搜索
func (c *QuakeClient) Search(ctx context.Context, query string, page, size int) (*QuakeResponse, error) {
	if c.apiKey == "" {
		return nil, fmt.Errorf("quake api key is empty")
	}

	// 构建请求体
	reqBody := map[string]interface{}{
		"query":        query,
		"start":        (page - 1) * size,
		"size":         size,
		"ignore_cache": false,
		"latest":       true,
	}

	data, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://quake.360.net/api/v3/search/quake_service", bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-QuakeToken", c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result QuakeResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if !result.Code.IsSuccess() {
		return nil, fmt.Errorf("quake error [%s]: %s", result.Code.Error(), result.Message)
	}

	return &result, nil
}

// SearchByIP 按IP搜索
func (c *QuakeClient) SearchByIP(ctx context.Context, ip string, page, size int) ([]QuakeData, error) {
	query := fmt.Sprintf(`ip:"%s"`, ip)
	result, err := c.Search(ctx, query, page, size)
	if err != nil {
		return nil, err
	}
	return result.Data.Items, nil
}

// SearchByDomain 按域名搜索
func (c *QuakeClient) SearchByDomain(ctx context.Context, domain string, page, size int) ([]QuakeData, error) {
	query := fmt.Sprintf(`domain:"%s"`, domain)
	result, err := c.Search(ctx, query, page, size)
	if err != nil {
		return nil, err
	}
	return result.Data.Items, nil
}

// SearchByTitle 按标题搜索
func (c *QuakeClient) SearchByTitle(ctx context.Context, title string, page, size int) ([]QuakeData, error) {
	query := fmt.Sprintf(`title:"%s"`, title)
	result, err := c.Search(ctx, query, page, size)
	if err != nil {
		return nil, err
	}
	return result.Data.Items, nil
}

// SearchByService 按服务搜索
func (c *QuakeClient) SearchByService(ctx context.Context, service string, page, size int) ([]QuakeData, error) {
	query := fmt.Sprintf(`service:"%s"`, service)
	result, err := c.Search(ctx, query, page, size)
	if err != nil {
		return nil, err
	}
	return result.Data.Items, nil
}
