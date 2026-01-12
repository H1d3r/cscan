package scanner

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Pipeline 扫描管道
// 提供统一的扫描流程控制，消除各扫描器中的重复逻辑
type Pipeline struct {
	name       string
	stages     []Stage
	logger     func(level, format string, args ...interface{})
	onProgress func(progress int, message string)
}

// Stage 管道阶段
type Stage struct {
	Name     string
	Weight   int // 进度权重（用于计算总进度）
	Execute  func(ctx context.Context, input *StageInput) (*StageOutput, error)
	Optional bool // 是否可选（失败时继续）
}

// StageInput 阶段输入
type StageInput struct {
	Target      string
	Targets     []string
	Assets      []*Asset
	Options     interface{}
	WorkspaceId string
	MainTaskId  string
	Data        map[string]interface{} // 阶段间传递的数据
}

// StageOutput 阶段输出
type StageOutput struct {
	Assets          []*Asset
	Vulnerabilities []*Vulnerability
	Data            map[string]interface{}
	Stopped         bool
	Message         string
}

// NewPipeline 创建扫描管道
func NewPipeline(name string) *Pipeline {
	return &Pipeline{
		name:   name,
		stages: make([]Stage, 0),
	}
}

// AddStage 添加阶段
func (p *Pipeline) AddStage(stage Stage) *Pipeline {
	p.stages = append(p.stages, stage)
	return p
}

// SetLogger 设置日志函数
func (p *Pipeline) SetLogger(logger func(level, format string, args ...interface{})) *Pipeline {
	p.logger = logger
	return p
}

// SetProgressCallback 设置进度回调
func (p *Pipeline) SetProgressCallback(callback func(progress int, message string)) *Pipeline {
	p.onProgress = callback
	return p
}

// Execute 执行管道
func (p *Pipeline) Execute(ctx context.Context, input *StageInput) (*ScanResult, error) {
	if input.Data == nil {
		input.Data = make(map[string]interface{})
	}

	result := &ScanResult{
		WorkspaceId: input.WorkspaceId,
		MainTaskId:  input.MainTaskId,
	}

	// 计算总权重
	totalWeight := 0
	for _, stage := range p.stages {
		if stage.Weight > 0 {
			totalWeight += stage.Weight
		} else {
			totalWeight += 1
		}
	}

	currentWeight := 0
	for _, stage := range p.stages {
		// 检查上下文取消
		select {
		case <-ctx.Done():
			p.log("info", "Pipeline %s cancelled", p.name)
			return result, ctx.Err()
		default:
		}

		// 计算进度
		weight := stage.Weight
		if weight <= 0 {
			weight = 1
		}
		progress := currentWeight * 100 / totalWeight
		p.reportProgress(progress, fmt.Sprintf("执行: %s", stage.Name))

		p.log("info", "[%s] Starting stage: %s", p.name, stage.Name)
		startTime := time.Now()

		// 执行阶段
		output, err := stage.Execute(ctx, input)
		duration := time.Since(startTime)

		if err != nil {
			p.log("error", "[%s] Stage %s failed: %v (%.2fs)", p.name, stage.Name, err, duration.Seconds())
			if !stage.Optional {
				return result, fmt.Errorf("stage %s failed: %w", stage.Name, err)
			}
			// 可选阶段失败，继续执行
			currentWeight += weight
			continue
		}

		if output == nil {
			p.log("warn", "[%s] Stage %s returned nil output (%.2fs)", p.name, stage.Name, duration.Seconds())
			currentWeight += weight
			continue
		}

		// 检查是否停止
		if output.Stopped {
			p.log("info", "[%s] Stage %s requested stop: %s", p.name, stage.Name, output.Message)
			return result, nil
		}

		// 合并结果
		if len(output.Assets) > 0 {
			result.Assets = append(result.Assets, output.Assets...)
			input.Assets = append(input.Assets, output.Assets...) // 传递给下一阶段
		}
		if len(output.Vulnerabilities) > 0 {
			result.Vulnerabilities = append(result.Vulnerabilities, output.Vulnerabilities...)
		}

		// 合并数据
		if output.Data != nil {
			for k, v := range output.Data {
				input.Data[k] = v
			}
		}

		p.log("info", "[%s] Stage %s completed: assets=%d, vuls=%d (%.2fs)",
			p.name, stage.Name, len(output.Assets), len(output.Vulnerabilities), duration.Seconds())

		currentWeight += weight
	}

	p.reportProgress(100, "完成")
	return result, nil
}

func (p *Pipeline) log(level, format string, args ...interface{}) {
	if p.logger != nil {
		p.logger(level, format, args...)
	}
}

func (p *Pipeline) reportProgress(progress int, message string) {
	if p.onProgress != nil {
		p.onProgress(progress, message)
	}
}

// ==================== 并发执行器 ====================

// ConcurrentExecutor 并发执行器
// 用于并发处理多个目标
type ConcurrentExecutor struct {
	concurrency int
	logger      func(level, format string, args ...interface{})
}

// NewConcurrentExecutor 创建并发执行器
func NewConcurrentExecutor(concurrency int) *ConcurrentExecutor {
	if concurrency <= 0 {
		concurrency = 10
	}
	return &ConcurrentExecutor{
		concurrency: concurrency,
	}
}

// SetLogger 设置日志函数
func (e *ConcurrentExecutor) SetLogger(logger func(level, format string, args ...interface{})) *ConcurrentExecutor {
	e.logger = logger
	return e
}

// Task 并发任务
type Task[T any, R any] struct {
	Input   T
	Execute func(ctx context.Context, input T) (R, error)
}

// Execute 并发执行任务
func (e *ConcurrentExecutor) Execute(ctx context.Context, tasks []Task[string, *Asset]) ([]*Asset, error) {
	if len(tasks) == 0 {
		return nil, nil
	}

	var results []*Asset
	var mu sync.Mutex
	var wg sync.WaitGroup

	// 创建任务通道
	taskChan := make(chan Task[string, *Asset], e.concurrency)

	// 启动工作协程
	for i := 0; i < e.concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range taskChan {
				select {
				case <-ctx.Done():
					return
				default:
					result, err := task.Execute(ctx, task.Input)
					if err != nil {
						if e.logger != nil {
							e.logger("debug", "Task failed for %s: %v", task.Input, err)
						}
						continue
					}
					if result != nil {
						mu.Lock()
						results = append(results, result)
						mu.Unlock()
					}
				}
			}
		}()
	}

	// 分发任务
	for _, task := range tasks {
		select {
		case <-ctx.Done():
			close(taskChan)
			wg.Wait()
			return results, ctx.Err()
		case taskChan <- task:
		}
	}

	close(taskChan)
	wg.Wait()

	return results, nil
}

// ExecuteGeneric 泛型并发执行
func ExecuteGeneric[T any, R any](ctx context.Context, concurrency int, inputs []T, fn func(context.Context, T) (R, error)) ([]R, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	if concurrency <= 0 {
		concurrency = 10
	}

	var results []R
	var mu sync.Mutex
	var wg sync.WaitGroup

	taskChan := make(chan T, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for input := range taskChan {
				select {
				case <-ctx.Done():
					return
				default:
					result, err := fn(ctx, input)
					if err != nil {
						continue
					}
					mu.Lock()
					results = append(results, result)
					mu.Unlock()
				}
			}
		}()
	}

	for _, input := range inputs {
		select {
		case <-ctx.Done():
			close(taskChan)
			wg.Wait()
			return results, ctx.Err()
		case taskChan <- input:
		}
	}

	close(taskChan)
	wg.Wait()

	return results, nil
}

// ==================== 结果收集器 ====================

// ResultCollector 结果收集器
// 线程安全的结果收集
type ResultCollector struct {
	assets []*Asset
	vuls   []*Vulnerability
	mu     sync.Mutex
}

// NewResultCollector 创建结果收集器
func NewResultCollector() *ResultCollector {
	return &ResultCollector{
		assets: make([]*Asset, 0),
		vuls:   make([]*Vulnerability, 0),
	}
}

// AddAsset 添加资产
func (c *ResultCollector) AddAsset(asset *Asset) {
	if asset == nil {
		return
	}
	c.mu.Lock()
	c.assets = append(c.assets, asset)
	c.mu.Unlock()
}

// AddAssets 批量添加资产
func (c *ResultCollector) AddAssets(assets []*Asset) {
	if len(assets) == 0 {
		return
	}
	c.mu.Lock()
	c.assets = append(c.assets, assets...)
	c.mu.Unlock()
}

// AddVulnerability 添加漏洞
func (c *ResultCollector) AddVulnerability(vul *Vulnerability) {
	if vul == nil {
		return
	}
	c.mu.Lock()
	c.vuls = append(c.vuls, vul)
	c.mu.Unlock()
}

// AddVulnerabilities 批量添加漏洞
func (c *ResultCollector) AddVulnerabilities(vuls []*Vulnerability) {
	if len(vuls) == 0 {
		return
	}
	c.mu.Lock()
	c.vuls = append(c.vuls, vuls...)
	c.mu.Unlock()
}

// GetAssets 获取所有资产
func (c *ResultCollector) GetAssets() []*Asset {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.assets
}

// GetVulnerabilities 获取所有漏洞
func (c *ResultCollector) GetVulnerabilities() []*Vulnerability {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.vuls
}

// ToScanResult 转换为扫描结果
func (c *ResultCollector) ToScanResult(workspaceId, mainTaskId string) *ScanResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	return &ScanResult{
		WorkspaceId:     workspaceId,
		MainTaskId:      mainTaskId,
		Assets:          c.assets,
		Vulnerabilities: c.vuls,
	}
}

// AssetCount 资产数量
func (c *ResultCollector) AssetCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.assets)
}

// VulCount 漏洞数量
func (c *ResultCollector) VulCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.vuls)
}
