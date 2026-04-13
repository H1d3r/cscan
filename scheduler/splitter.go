package scheduler

import (
	"cscan/scanner"
	"fmt"
	"strings"
)

// ChunkConfig 分片配置
type ChunkConfig struct {
	MaxTargetsPerChunk int  `json:"maxTargetsPerChunk"` // 每个分片的最大目标数
	EnableChunking     bool `json:"enableChunking"`     // 是否启用分片
	MinChunkSize       int  `json:"minChunkSize"`       // 最小分片大小
	MaxChunkSize       int  `json:"maxChunkSize"`       // 最大分片大小
}

// DefaultChunkConfig 默认分片配置
func DefaultChunkConfig() *ChunkConfig {
	return &ChunkConfig{
		MaxTargetsPerChunk: 30, // 默认每个分片30个目标
		EnableChunking:     true,
		MinChunkSize:       10,  // 最小10个目标
		MaxChunkSize:       100, // 最大100个目标
	}
}

// TaskSplitter 任务拆分器（增强版）
type TaskSplitter struct {
	config *ChunkConfig
}

// NewTaskSplitter 创建任务拆分器
func NewTaskSplitter(config *ChunkConfig) *TaskSplitter {
	if config == nil {
		config = DefaultChunkConfig()
	}

	// 验证配置参数
	if config.MaxTargetsPerChunk <= 0 {
		config.MaxTargetsPerChunk = 30
	}
	if config.MinChunkSize <= 0 {
		config.MinChunkSize = 10
	}
	if config.MaxChunkSize <= 0 {
		config.MaxChunkSize = 100
	}
	if config.MinChunkSize > config.MaxChunkSize {
		config.MinChunkSize = config.MaxChunkSize
	}

	return &TaskSplitter{config: config}
}

// SplitResult 拆分结果
type SplitResult struct {
	Chunks          []TaskChunk `json:"chunks"`          // 分片列表
	TotalTargets    int         `json:"totalTargets"`    // 总目标数
	ChunkCount      int         `json:"chunkCount"`      // 分片数量
	NeedSplit       bool        `json:"needSplit"`       // 是否需要拆分
	EstimatedTime   int         `json:"estimatedTime"`   // 预估执行时间（秒）
	RecommendedSize int         `json:"recommendedSize"` // 推荐的分片大小
}

// TaskChunk 任务分片
type TaskChunk struct {
	Index       int      `json:"index"`       // 分片索引（从0开始）
	Targets     []string `json:"targets"`     // 目标列表
	TargetCount int      `json:"targetCount"` // 目标数量
	ChunkId     string   `json:"chunkId"`     // 分片ID
	Priority    int      `json:"priority"`    // 优先级
}

// SplitTask 拆分任务
func (s *TaskSplitter) SplitTask(taskId, target string, taskConfig map[string]interface{}) (*SplitResult, error) {
	// 解析所有目标
	allTargets, err := s.parseAllTargets(target)
	if err != nil {
		return nil, fmt.Errorf("解析目标失败: %v", err)
	}

	totalTargets := len(allTargets)

	// 检查是否需要拆分
	needSplit := s.config.EnableChunking && totalTargets > s.config.MaxTargetsPerChunk

	result := &SplitResult{
		TotalTargets:    totalTargets,
		NeedSplit:       needSplit,
		RecommendedSize: s.calculateOptimalChunkSize(totalTargets),
	}

	// 如果不需要拆分，返回单个分片
	if !needSplit {
		chunk := TaskChunk{
			Index:       0,
			Targets:     allTargets,
			TargetCount: totalTargets,
			ChunkId:     taskId,
			Priority:    1,
		}
		result.Chunks = []TaskChunk{chunk}
		result.ChunkCount = 1
		result.EstimatedTime = s.estimateExecutionTime(totalTargets, taskConfig)
		return result, nil
	}

	// 拆分为多个分片
	chunks := s.createChunks(taskId, allTargets)
	result.Chunks = chunks
	result.ChunkCount = len(chunks)
	result.EstimatedTime = s.estimateExecutionTime(totalTargets, taskConfig)

	return result, nil
}

// calculateOptimalChunkSize 计算最优分片大小
func (s *TaskSplitter) calculateOptimalChunkSize(totalTargets int) int {
	if totalTargets <= s.config.MinChunkSize {
		return totalTargets
	}

	// 基于目标数量动态调整分片大小
	optimalSize := s.config.MaxTargetsPerChunk

	// 如果目标数量很大，适当增加分片大小以减少分片数量
	if totalTargets > 1000 {
		optimalSize = s.config.MaxChunkSize
	} else if totalTargets > 500 {
		optimalSize = (s.config.MaxTargetsPerChunk + s.config.MaxChunkSize) / 2
	}

	// 确保分片大小在合理范围内
	if optimalSize < s.config.MinChunkSize {
		optimalSize = s.config.MinChunkSize
	}
	if optimalSize > s.config.MaxChunkSize {
		optimalSize = s.config.MaxChunkSize
	}

	return optimalSize
}

// createChunks 创建分片
func (s *TaskSplitter) createChunks(taskId string, allTargets []string) []TaskChunk {
	var chunks []TaskChunk
	chunkSize := s.calculateOptimalChunkSize(len(allTargets))

	for i := 0; i < len(allTargets); i += chunkSize {
		end := i + chunkSize
		if end > len(allTargets) {
			end = len(allTargets)
		}

		chunkTargets := allTargets[i:end]
		chunkId := taskId
		if len(allTargets) > chunkSize {
			chunkId = fmt.Sprintf("%s-chunk-%d", taskId, len(chunks))
		}

		chunk := TaskChunk{
			Index:       len(chunks),
			Targets:     chunkTargets,
			TargetCount: len(chunkTargets),
			ChunkId:     chunkId,
			Priority:    s.calculateChunkPriority(len(chunks), len(chunkTargets)),
		}

		chunks = append(chunks, chunk)
	}

	return chunks
}

// calculateChunkPriority 计算分片优先级
func (s *TaskSplitter) calculateChunkPriority(index, targetCount int) int {
	// 基础优先级
	basePriority := 1

	// 目标数量少的分片优先级更高（更快完成）
	if targetCount <= s.config.MinChunkSize {
		basePriority += 2
	} else if targetCount <= s.config.MaxTargetsPerChunk {
		basePriority += 1
	}

	// 前面的分片优先级稍高
	if index < 3 {
		basePriority += 1
	}

	return basePriority
}

// estimateExecutionTime 估算执行时间
func (s *TaskSplitter) estimateExecutionTime(targetCount int, taskConfig map[string]interface{}) int {
	// 基础时间：每个目标30秒
	baseTimePerTarget := 30

	// 根据启用的扫描模块调整时间
	multiplier := 1.0
	if config, ok := taskConfig["portscan"].(map[string]interface{}); ok {
		if enable, ok := config["enable"].(bool); ok && enable {
			multiplier += 0.5
		}
	}
	if config, ok := taskConfig["fingerprint"].(map[string]interface{}); ok {
		if enable, ok := config["enable"].(bool); ok && enable {
			multiplier += 0.3
		}
	}
	if config, ok := taskConfig["pocscan"].(map[string]interface{}); ok {
		if enable, ok := config["enable"].(bool); ok && enable {
			multiplier += 1.0
		}
	}
	if config, ok := taskConfig["dirscan"].(map[string]interface{}); ok {
		if enable, ok := config["enable"].(bool); ok && enable {
			multiplier += 0.8
		}
	}

	return int(float64(targetCount*baseTimePerTarget) * multiplier)
}

// parseAllTargets 解析所有目标（展开CIDR和IP范围）
func (s *TaskSplitter) parseAllTargets(target string) ([]string, error) {
	parser := scanner.NewTargetParser()
	targets := parser.ExpandAll(target)
	if len(targets) == 0 {
		return []string{}, fmt.Errorf("没有在此输入中解析到有效的扫描目标或IP")
	}
	return targets, nil
}

// GetSplitPreview 获取拆分预览（不实际拆分）
func (s *TaskSplitter) GetSplitPreview(target string, taskConfig map[string]interface{}) (*SplitPreview, error) {
	allTargets, err := s.parseAllTargets(target)
	if err != nil {
		return nil, err
	}

	totalTargets := len(allTargets)
	needSplit := s.config.EnableChunking && totalTargets > s.config.MaxTargetsPerChunk

	chunkSize := s.calculateOptimalChunkSize(totalTargets)
	chunkCount := 1
	if needSplit {
		chunkCount = (totalTargets + chunkSize - 1) / chunkSize
	}

	return &SplitPreview{
		TotalTargets:     totalTargets,
		ChunkCount:       chunkCount,
		ChunkSize:        chunkSize,
		NeedSplit:        needSplit,
		EstimatedTime:    s.estimateExecutionTime(totalTargets, taskConfig),
		RecommendedSize:  chunkSize,
		MaxMemoryUsage:   s.estimateMemoryUsage(totalTargets),
		ParallelCapacity: s.calculateParallelCapacity(chunkCount),
	}, nil
}

// SplitPreview 拆分预览
type SplitPreview struct {
	TotalTargets     int     `json:"totalTargets"`     // 总目标数
	ChunkCount       int     `json:"chunkCount"`       // 分片数量
	ChunkSize        int     `json:"chunkSize"`        // 分片大小
	NeedSplit        bool    `json:"needSplit"`        // 是否需要拆分
	EstimatedTime    int     `json:"estimatedTime"`    // 预估执行时间（秒）
	RecommendedSize  int     `json:"recommendedSize"`  // 推荐分片大小
	MaxMemoryUsage   float64 `json:"maxMemoryUsage"`   // 预估最大内存使用（MB）
	ParallelCapacity int     `json:"parallelCapacity"` // 并行处理能力
}

// estimateMemoryUsage 估算内存使用
func (s *TaskSplitter) estimateMemoryUsage(targetCount int) float64 {
	// 每个目标大约占用1KB内存
	return float64(targetCount) / 1024.0
}

// calculateParallelCapacity 计算并行处理能力
func (s *TaskSplitter) calculateParallelCapacity(chunkCount int) int {
	// 基于分片数量计算可以并行处理的Worker数量
	if chunkCount <= 5 {
		return chunkCount
	} else if chunkCount <= 20 {
		return 5
	} else {
		return 10
	}
}

// ValidateChunkConfig 验证分片配置
func ValidateChunkConfig(config *ChunkConfig) error {
	if config == nil {
		return fmt.Errorf("分片配置不能为空")
	}

	if config.MaxTargetsPerChunk <= 0 {
		return fmt.Errorf("每个分片的最大目标数必须大于0")
	}

	if config.MinChunkSize <= 0 {
		return fmt.Errorf("最小分片大小必须大于0")
	}

	if config.MaxChunkSize <= 0 {
		return fmt.Errorf("最大分片大小必须大于0")
	}

	if config.MinChunkSize > config.MaxChunkSize {
		return fmt.Errorf("最小分片大小不能大于最大分片大小")
	}

	if config.MaxTargetsPerChunk > config.MaxChunkSize {
		return fmt.Errorf("每个分片的最大目标数不能大于最大分片大小")
	}

	return nil
}

// TargetSplitter 目标拆分器（保持向后兼容）
type TargetSplitter struct {
	batchSize int // 每批次的IP数量
}

// NewTargetSplitter 创建目标拆分器（保持向后兼容）
func NewTargetSplitter(batchSize int) *TargetSplitter {
	if batchSize <= 0 {
		batchSize = 50 // 默认每批50个IP
	}
	return &TargetSplitter{batchSize: batchSize}
}

// SplitTargets 拆分目标为多个批次（保持向后兼容）
func (s *TargetSplitter) SplitTargets(target string) []string {
	// 使用新的TaskSplitter实现
	config := &ChunkConfig{
		MaxTargetsPerChunk: s.batchSize,
		EnableChunking:     true,
		MinChunkSize:       10,
		MaxChunkSize:       s.batchSize * 2,
	}

	splitter := NewTaskSplitter(config)
	result, err := splitter.SplitTask("", target, nil)
	if err != nil || !result.NeedSplit {
		return []string{target}
	}

	var batches []string
	for _, chunk := range result.Chunks {
		batch := strings.Join(chunk.Targets, "\n")
		batches = append(batches, batch)
	}

	return batches
}

// GetTargetCount 获取目标总数（不展开）
func (s *TargetSplitter) GetTargetCount(target string) int {
	splitter := NewTaskSplitter(nil)
	targets, _ := splitter.parseAllTargets(target)
	return len(targets)
}

// NeedSplit 判断是否需要拆分
func (s *TargetSplitter) NeedSplit(target string) bool {
	return s.GetTargetCount(target) > s.batchSize
}
