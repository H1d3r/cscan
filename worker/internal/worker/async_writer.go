package worker

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"cscan/internal/scanner"
)

// ==================== 扫描结果异步批量落库（Async Bulk Persist） ====================
//
// 背景：此前资产/证书/漏洞/JS/目录扫描结果由扫描主链路【同步】直写 MongoDB
// （MongoDirect）：每条资产 4+ 次往返、证书逐张 EnsureIndexes+Upsert，
// 并发=5 时迅速耗尽 Worker 独享的 20 连接池，产生大量 "context deadline
// exceeded"，并连带饿死任务状态回写（current_phase / progress /
// MarkTaskCompleted 共用同一连接池），平台侧表现为任务"卡在 Fingerprint 终止"。
//
// AsyncResultWriter 将"扫描产生结果"与"结果落库"解耦：
//   - 扫描主链路仅把结果投递到有界缓冲 channel（非阻塞；满时回退同步路径）；
//   - 单一后台协程按 mainTask 分组累积，定量（资产 50/批，其余 100/批）
//     或定时（默认 3s）批量调用直写回调；
//   - 批量写入失败仍由回调内部（saveXxxSyncOrQueue）落 ResultQueue 本地文件，
//     不丢数据、不阻塞扫描主链路；
//   - Stop() 排空缓冲后退出，进程优雅停机不丢尾部数据；
//   - 代价：硬崩溃（kill -9 / OOM）可能丢失最近一个 flush 间隔（默认 3s）
//     内缓冲的结果 —— 相比当前"同步写被过载拖垮导致任务状态丢失/错乱"，
//     属于可接受的权衡；优雅停机路径无此问题。

// asyncWriteKind 异步写请求类型
type asyncWriteKind int

const (
	asyncWriteAssets asyncWriteKind = iota
	asyncWriteCerts
	asyncWriteVuls
	asyncWriteJS
	asyncWriteDirScan
)

// asyncWriteRequest 一次异步写请求（引用调用方结果切片，由后台协程统一批量落库）
type asyncWriteRequest struct {
	kind       asyncWriteKind
	mainTaskID string
	orgID      string
	assets     []*scanner.Asset
	certs      []*scanner.CertResult
	vuls       []*scanner.Vulnerability
	jsResults  []*JSFinderResultItem
	dirResults []DirScanResultDocument
}

// AsyncWriteCallbacks 批量落库回调（同步直写 + 失败入 ResultQueue，不投递回异步通道避免自循环）
type AsyncWriteCallbacks struct {
	SaveAssets  func(ctx context.Context, mainTaskID, orgID string, assets []*scanner.Asset) error
	SaveCerts   func(ctx context.Context, mainTaskID string, certs []*scanner.CertResult) error
	SaveVuls    func(ctx context.Context, mainTaskID string, vuls []*scanner.Vulnerability) error
	SaveJS      func(ctx context.Context, mainTaskID string, results []*JSFinderResultItem) error
	SaveDirScan func(ctx context.Context, mainTaskID string, results []DirScanResultDocument) error
}

// AsyncWriterConfig 异步批量写配置
type AsyncWriterConfig struct {
	ChanSize       int           // 投递通道容量；满时调用方回退同步直写（保证有界内存）
	FlushInterval  time.Duration // 定时 flush 间隔
	FlushTimeout   time.Duration // 单批写入超时（超时由回调内部落 ResultQueue 兜底）
	MaxAssetsBatch int           // 单批资产条数上限（与 OOM 硬约束 maxBatchSize=50 对齐）
	MaxOtherBatch  int           // 单批证书/漏洞/JS/目录条数上限
}

// defaultAsyncWriterConfig 默认配置
// ChanSize=1024：以"请求"为单位的有界缓冲（多数请求 ≤10 条资产），
// MongoDB 故障时 flush 以 FlushTimeout 为上界逐批失败落盘，通道打满后
// 生产方自动回退同步路径，内存不会无界增长。
func defaultAsyncWriterConfig() AsyncWriterConfig {
	return AsyncWriterConfig{
		ChanSize:       1024,
		FlushInterval:  3 * time.Second,
		FlushTimeout:   120 * time.Second,
		MaxAssetsBatch: 50,
		MaxOtherBatch:  100,
	}
}

// asyncPendingBatch 按 mainTask(+orgID) 分组的待写批次
type asyncPendingBatch struct {
	mainTaskID string
	orgID      string
	assets     []*scanner.Asset
	certs      []*scanner.CertResult
	vuls       []*scanner.Vulnerability
	jsResults  []*JSFinderResultItem
	dirResults []DirScanResultDocument
}

// AsyncResultWriter 扫描结果异步批量落库器
type AsyncResultWriter struct {
	cfg    AsyncWriterConfig
	cb     AsyncWriteCallbacks
	logger func(level, format string, args ...interface{})

	ch       chan *asyncWriteRequest
	stopChan chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup

	// mu 保护 stopped 与 channel 发送的互斥：Stop 置位后不再有生产者写入，
	// run() 的排空循环看到空通道即真正为空，杜绝"排空后又写入"的丢失窗口。
	mu      sync.RWMutex
	stopped bool

	totalEnqueued uint64 // 原子计数：已接受投递的请求数
	totalFlushed  uint64 // 原子计数：已 flush 的批次块数（观测用）
}

// NewAsyncResultWriter 创建异步批量写协程（构造后需 Start 才开始消费）
func NewAsyncResultWriter(cfg AsyncWriterConfig, cb AsyncWriteCallbacks) *AsyncResultWriter {
	if cfg.ChanSize <= 0 {
		cfg.ChanSize = 1024
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 3 * time.Second
	}
	if cfg.FlushTimeout <= 0 {
		cfg.FlushTimeout = 120 * time.Second
	}
	if cfg.MaxAssetsBatch <= 0 {
		cfg.MaxAssetsBatch = 50
	}
	if cfg.MaxOtherBatch <= 0 {
		cfg.MaxOtherBatch = 100
	}
	return &AsyncResultWriter{
		cfg:      cfg,
		cb:       cb,
		ch:       make(chan *asyncWriteRequest, cfg.ChanSize),
		stopChan: make(chan struct{}),
	}
}

// SetLogger 设置日志回调
func (w *AsyncResultWriter) SetLogger(logger func(level, format string, args ...interface{})) {
	w.logger = logger
}

func (w *AsyncResultWriter) log(level, format string, args ...interface{}) {
	if w.logger != nil {
		w.logger(level, format, args...)
	}
}

// Enqueue 投递一次写请求。返回 false 表示写协程已停止或通道已满，
// 调用方应回退到同步直写路径（保证零丢失、不阻塞扫描）。
func (w *AsyncResultWriter) Enqueue(req *asyncWriteRequest) bool {
	if req == nil {
		return false
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.stopped {
		return false
	}
	select {
	case w.ch <- req:
		atomic.AddUint64(&w.totalEnqueued, 1)
		return true
	default:
		// 通道满：不阻塞扫描主链路，由调用方回退同步直写
		return false
	}
}

// Start 启动后台批量写协程
func (w *AsyncResultWriter) Start() {
	w.wg.Add(1)
	go w.run()
}

// Stop 停止：置位拒绝新请求 → 唤醒写协程 → 排空缓冲并落库 → 等待退出。
// 必须在 MongoDB 连接断开之前调用，否则尾部缓冲无法落库。
func (w *AsyncResultWriter) Stop() {
	w.mu.Lock()
	w.stopped = true
	w.mu.Unlock()

	w.stopOnce.Do(func() { close(w.stopChan) })
	w.wg.Wait()

	w.log("INFO", "async writer stopped: enqueued=%d, flushed=%d",
		atomic.LoadUint64(&w.totalEnqueued), atomic.LoadUint64(&w.totalFlushed))
}

// run 后台主循环：消费请求、按任务分组累积、定时/定量 flush
func (w *AsyncResultWriter) run() {
	batches := make(map[string]*asyncPendingBatch)

	defer w.wg.Done()
	defer func() {
		if r := recover(); r != nil {
			w.log("ERROR", "async writer panic recovered: %v, best-effort flushing remaining batches", r)
			func() {
				defer func() {
					if r2 := recover(); r2 != nil {
						w.log("ERROR", "async writer recovery flush panic: %v", r2)
					}
				}()
				w.flushBatches(batches)
			}()
		}
	}()

	ticker := time.NewTicker(w.cfg.FlushInterval)
	defer ticker.Stop()

	for {
		select {
		case req := <-w.ch:
			w.appendRequest(batches, req)
			if w.shouldFlush(batches) {
				w.flushBatches(batches)
			}
		case <-ticker.C:
			w.flushBatches(batches)
		case <-w.stopChan:
			// 排空通道内剩余请求（stopped 已置位，不会再有新写入）
			for {
				select {
				case req := <-w.ch:
					w.appendRequest(batches, req)
				default:
					w.flushBatches(batches)
					return
				}
			}
		}
	}
}

// asyncBatchKey 以 mainTaskID+orgID 分组（不同主任务互不混批，避免 SaveAssets 跨任务串写）
func asyncBatchKey(mainTaskID, orgID string) string {
	return mainTaskID + "\x00" + orgID
}

// appendRequest 将请求累积进对应任务的批次
func (w *AsyncResultWriter) appendRequest(batches map[string]*asyncPendingBatch, req *asyncWriteRequest) {
	key := asyncBatchKey(req.mainTaskID, req.orgID)
	b, ok := batches[key]
	if !ok {
		b = &asyncPendingBatch{mainTaskID: req.mainTaskID, orgID: req.orgID}
		batches[key] = b
	}
	switch req.kind {
	case asyncWriteAssets:
		b.assets = append(b.assets, req.assets...)
	case asyncWriteCerts:
		b.certs = append(b.certs, req.certs...)
	case asyncWriteVuls:
		b.vuls = append(b.vuls, req.vuls...)
	case asyncWriteJS:
		b.jsResults = append(b.jsResults, req.jsResults...)
	case asyncWriteDirScan:
		b.dirResults = append(b.dirResults, req.dirResults...)
	}
}

// shouldFlush 判断是否达到定量阈值（任一任务的任一类型达到上限即触发全量 flush）
func (w *AsyncResultWriter) shouldFlush(batches map[string]*asyncPendingBatch) bool {
	for _, b := range batches {
		if len(b.assets) >= w.cfg.MaxAssetsBatch ||
			len(b.certs) >= w.cfg.MaxOtherBatch ||
			len(b.vuls) >= w.cfg.MaxOtherBatch ||
			len(b.jsResults) >= w.cfg.MaxOtherBatch ||
			len(b.dirResults) >= w.cfg.MaxOtherBatch {
			return true
		}
	}
	return false
}

// flushBatches 将所有待写批次落库后清空
func (w *AsyncResultWriter) flushBatches(batches map[string]*asyncPendingBatch) {
	if len(batches) == 0 {
		return
	}
	for _, b := range batches {
		w.flushBatch(b)
	}
	for k := range batches {
		delete(batches, k)
	}
}

// flushBatch 落库单个任务的批次：资产按 MaxAssetsBatch 切块、其余按 MaxOtherBatch 切块，
// 与既有 OOM 硬约束（maxBatchSize=50 / maxBatchBytes=1MB）保持一致的单批上界。
func (w *AsyncResultWriter) flushBatch(b *asyncPendingBatch) {
	for start := 0; start < len(b.assets); start += w.cfg.MaxAssetsBatch {
		end := start + w.cfg.MaxAssetsBatch
		if end > len(b.assets) {
			end = len(b.assets)
		}
		chunk := b.assets[start:end]
		if err := w.withFlushTimeout(func(ctx context.Context) error {
			return w.cb.SaveAssets(ctx, b.mainTaskID, b.orgID, chunk)
		}); err != nil {
			w.log("ERROR", "save assets batch failed (task=%s, n=%d): %v", b.mainTaskID, len(chunk), err)
		}
		atomic.AddUint64(&w.totalFlushed, 1)
	}

	for start := 0; start < len(b.certs); start += w.cfg.MaxOtherBatch {
		end := start + w.cfg.MaxOtherBatch
		if end > len(b.certs) {
			end = len(b.certs)
		}
		chunk := b.certs[start:end]
		if err := w.withFlushTimeout(func(ctx context.Context) error {
			return w.cb.SaveCerts(ctx, b.mainTaskID, chunk)
		}); err != nil {
			w.log("ERROR", "save certs batch failed (task=%s, n=%d): %v", b.mainTaskID, len(chunk), err)
		}
		atomic.AddUint64(&w.totalFlushed, 1)
	}

	for start := 0; start < len(b.vuls); start += w.cfg.MaxOtherBatch {
		end := start + w.cfg.MaxOtherBatch
		if end > len(b.vuls) {
			end = len(b.vuls)
		}
		chunk := b.vuls[start:end]
		if err := w.withFlushTimeout(func(ctx context.Context) error {
			return w.cb.SaveVuls(ctx, b.mainTaskID, chunk)
		}); err != nil {
			w.log("ERROR", "save vuls batch failed (task=%s, n=%d): %v", b.mainTaskID, len(chunk), err)
		}
		atomic.AddUint64(&w.totalFlushed, 1)
	}

	for start := 0; start < len(b.jsResults); start += w.cfg.MaxOtherBatch {
		end := start + w.cfg.MaxOtherBatch
		if end > len(b.jsResults) {
			end = len(b.jsResults)
		}
		chunk := b.jsResults[start:end]
		if err := w.withFlushTimeout(func(ctx context.Context) error {
			return w.cb.SaveJS(ctx, b.mainTaskID, chunk)
		}); err != nil {
			w.log("ERROR", "save js results batch failed (task=%s, n=%d): %v", b.mainTaskID, len(chunk), err)
		}
		atomic.AddUint64(&w.totalFlushed, 1)
	}

	for start := 0; start < len(b.dirResults); start += w.cfg.MaxOtherBatch {
		end := start + w.cfg.MaxOtherBatch
		if end > len(b.dirResults) {
			end = len(b.dirResults)
		}
		chunk := b.dirResults[start:end]
		if err := w.withFlushTimeout(func(ctx context.Context) error {
			return w.cb.SaveDirScan(ctx, b.mainTaskID, chunk)
		}); err != nil {
			w.log("ERROR", "save dirscan batch failed (task=%s, n=%d): %v", b.mainTaskID, len(chunk), err)
		}
		atomic.AddUint64(&w.totalFlushed, 1)
	}
}

// withFlushTimeout 为单批写入构造独立超时上下文。
// 不复用扫描阶段 ctx：fpCtx 等阶段级取消信号不应中断已入队结果的落库。
func (w *AsyncResultWriter) withFlushTimeout(fn func(ctx context.Context) error) error {
	if w.cfg.FlushTimeout <= 0 {
		return fn(context.Background())
	}
	ctx, cancel := context.WithTimeout(context.Background(), w.cfg.FlushTimeout)
	defer cancel()
	return fn(ctx)
}
