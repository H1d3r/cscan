package worker

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cscan/internal/scanner"
)

// asyncWriterTestHarness 记录 flush 回调收到的批次
type asyncWriterTestHarness struct {
	mu        sync.Mutex
	assetRows []assetRow
	certCount int
	vulCount  int
	flushes   int
}

type assetRow struct {
	task  string
	orgID string
	n     int
}

func (h *asyncWriterTestHarness) saveAssets(ctx context.Context, mainTaskID, orgID string, assets []*scanner.Asset) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.assetRows = append(h.assetRows, assetRow{task: mainTaskID, orgID: orgID, n: len(assets)})
	return nil
}

func (h *asyncWriterTestHarness) saveCerts(ctx context.Context, mainTaskID string, certs []*scanner.CertResult) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.certCount += len(certs)
	h.flushes++
	return nil
}

func (h *asyncWriterTestHarness) saveVuls(ctx context.Context, mainTaskID string, vuls []*scanner.Vulnerability) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.vulCount += len(vuls)
	return nil
}

func (h *asyncWriterTestHarness) saveJS(ctx context.Context, mainTaskID string, results []*JSFinderResultItem) error {
	return nil
}

func (h *asyncWriterTestHarness) saveDirScan(ctx context.Context, mainTaskID string, results []DirScanResultDocument) error {
	return nil
}

func newTestAsyncWriter(h *asyncWriterTestHarness, cfg AsyncWriterConfig) *AsyncResultWriter {
	w := NewAsyncResultWriter(cfg, AsyncWriteCallbacks{
		SaveAssets:  h.saveAssets,
		SaveCerts:   h.saveCerts,
		SaveVuls:    h.saveVuls,
		SaveJS:      h.saveJS,
		SaveDirScan: h.saveDirScan,
	})
	return w
}

// TestAsyncResultWriter_BatchByInterval 定时 flush：零散请求在 flush 间隔内聚合为单批
func TestAsyncResultWriter_BatchByInterval(t *testing.T) {
	h := &asyncWriterTestHarness{}
	w := newTestAsyncWriter(h, AsyncWriterConfig{
		ChanSize:       64,
		FlushInterval:  100 * time.Millisecond,
		FlushTimeout:   5 * time.Second,
		MaxAssetsBatch: 50,
		MaxOtherBatch:  100,
	})
	w.Start()

	for i := 0; i < 7; i++ {
		ok := w.Enqueue(&asyncWriteRequest{
			kind:       asyncWriteAssets,
			mainTaskID: "task-A",
			orgID:      "org1",
			assets:     []*scanner.Asset{{Host: "a.example.com", Port: 80}},
		})
		if !ok {
			t.Fatalf("enqueue %d rejected", i)
		}
	}

	// 等待至少一次定时 flush
	time.Sleep(400 * time.Millisecond)
	w.Stop()

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.assetRows) == 0 {
		t.Fatal("expected at least one flushed batch, got none")
	}
	total := 0
	for _, r := range h.assetRows {
		if r.task != "task-A" || r.orgID != "org1" {
			t.Fatalf("unexpected batch grouping: %+v", r)
		}
		total += r.n
	}
	if total != 7 {
		t.Fatalf("expected 7 assets flushed, got %d", total)
	}
}

// TestAsyncResultWriter_StopDrainsBuffer Stop 排空：入队后立即 Stop，缓冲仍必须落库
func TestAsyncResultWriter_StopDrainsBuffer(t *testing.T) {
	h := &asyncWriterTestHarness{}
	w := newTestAsyncWriter(h, AsyncWriterConfig{
		ChanSize:       64,
		FlushInterval:  time.Hour, // 关闭定时 flush，全部依赖 Stop 排空
		FlushTimeout:   5 * time.Second,
		MaxAssetsBatch: 50,
		MaxOtherBatch:  100,
	})
	w.Start()

	for i := 0; i < 12; i++ {
		w.Enqueue(&asyncWriteRequest{
			kind:       asyncWriteAssets,
			mainTaskID: "task-B",
			orgID:      "org1",
			assets:     []*scanner.Asset{{Host: "b.example.com", Port: 443}},
		})
	}

	w.Stop() // 必须排空 12 条

	h.mu.Lock()
	defer h.mu.Unlock()
	total := 0
	for _, r := range h.assetRows {
		total += r.n
	}
	if total != 12 {
		t.Fatalf("Stop should drain all 12 buffered assets, got %d", total)
	}
}

// TestAsyncResultWriter_BatchSizeCap 定量切块：单批资产不超过 MaxAssetsBatch
func TestAsyncResultWriter_BatchSizeCap(t *testing.T) {
	h := &asyncWriterTestHarness{}
	w := newTestAsyncWriter(h, AsyncWriterConfig{
		ChanSize:       64,
		FlushInterval:  time.Hour,
		FlushTimeout:   5 * time.Second,
		MaxAssetsBatch: 50,
		MaxOtherBatch:  100,
	})
	w.Start()

	// 一次投递 130 条，应切成 50+50+30
	big := make([]*scanner.Asset, 130)
	for i := range big {
		big[i] = &scanner.Asset{Host: "c.example.com", Port: 8080}
	}
	w.Enqueue(&asyncWriteRequest{
		kind:       asyncWriteAssets,
		mainTaskID: "task-C",
		orgID:      "org1",
		assets:     big,
	})

	w.Stop()

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.assetRows) != 3 {
		t.Fatalf("expected 3 chunks (50/50/30), got %d", len(h.assetRows))
	}
	if h.assetRows[0].n != 50 || h.assetRows[1].n != 50 || h.assetRows[2].n != 30 {
		t.Fatalf("unexpected chunk sizes: %+v", h.assetRows)
	}
}

// TestAsyncResultWriter_RejectAfterStop Stop 后 Enqueue 必须拒绝（调用方回退同步路径）
func TestAsyncResultWriter_RejectAfterStop(t *testing.T) {
	h := &asyncWriterTestHarness{}
	w := newTestAsyncWriter(h, defaultAsyncWriterConfig())
	w.Start()
	w.Stop()

	if w.Enqueue(&asyncWriteRequest{
		kind:       asyncWriteAssets,
		mainTaskID: "task-D",
		orgID:      "org1",
		assets:     []*scanner.Asset{{Host: "d.example.com", Port: 80}},
	}) {
		t.Fatal("Enqueue must return false after Stop")
	}
}

// TestAsyncResultWriter_FullChannelFallback 通道打满时 Enqueue 返回 false（回退同步直写）
func TestAsyncResultWriter_FullChannelFallback(t *testing.T) {
	h := &asyncWriterTestHarness{}
	// 不 Start：通道无人消费，填满后应拒绝
	w := newTestAsyncWriter(h, AsyncWriterConfig{
		ChanSize:       2,
		FlushInterval:  time.Hour,
		FlushTimeout:   5 * time.Second,
		MaxAssetsBatch: 50,
		MaxOtherBatch:  100,
	})

	ok1 := w.Enqueue(&asyncWriteRequest{kind: asyncWriteAssets, mainTaskID: "t", assets: []*scanner.Asset{{}}})
	ok2 := w.Enqueue(&asyncWriteRequest{kind: asyncWriteAssets, mainTaskID: "t", assets: []*scanner.Asset{{}}})
	ok3 := w.Enqueue(&asyncWriteRequest{kind: asyncWriteAssets, mainTaskID: "t", assets: []*scanner.Asset{{}}})
	if !ok1 || !ok2 {
		t.Fatal("first two enqueues should succeed")
	}
	if ok3 {
		t.Fatal("third enqueue must be rejected when channel is full")
	}
}

// TestAsyncResultWriter_MultiTaskGrouping 不同主任务不混批
func TestAsyncResultWriter_MultiTaskGrouping(t *testing.T) {
	h := &asyncWriterTestHarness{}
	w := newTestAsyncWriter(h, AsyncWriterConfig{
		ChanSize:       64,
		FlushInterval:  time.Hour,
		FlushTimeout:   5 * time.Second,
		MaxAssetsBatch: 50,
		MaxOtherBatch:  100,
	})
	w.Start()

	w.Enqueue(&asyncWriteRequest{kind: asyncWriteAssets, mainTaskID: "task-E", orgID: "o1", assets: []*scanner.Asset{{Host: "e1"}}})
	w.Enqueue(&asyncWriteRequest{kind: asyncWriteAssets, mainTaskID: "task-F", orgID: "o1", assets: []*scanner.Asset{{Host: "f1"}}})
	w.Enqueue(&asyncWriteRequest{kind: asyncWriteAssets, mainTaskID: "task-E", orgID: "o1", assets: []*scanner.Asset{{Host: "e2"}}})

	w.Stop()

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.assetRows) != 2 {
		t.Fatalf("expected 2 batches (per main task), got %d", len(h.assetRows))
	}
	for _, r := range h.assetRows {
		want := 2
		if r.task == "task-F" {
			want = 1
		}
		if r.n != want {
			t.Fatalf("task=%s expected %d assets, got %d", r.task, want, r.n)
		}
	}
}

// TestAsyncResultWriter_QuantitativeFlush 定量触发：达到 MaxOtherBatch 立即 flush，无需等定时器
func TestAsyncResultWriter_QuantitativeFlush(t *testing.T) {
	h := &asyncWriterTestHarness{}
	w := newTestAsyncWriter(h, AsyncWriterConfig{
		ChanSize:       1024,
		FlushInterval:  time.Hour, // 关闭定时，验证定量触发
		FlushTimeout:   5 * time.Second,
		MaxAssetsBatch: 50,
		MaxOtherBatch:  10,
	})
	w.Start()

	certs := make([]*scanner.CertResult, 10)
	for i := range certs {
		certs[i] = &scanner.CertResult{Host: "cert.example.com", Port: 443}
	}
	w.Enqueue(&asyncWriteRequest{kind: asyncWriteCerts, mainTaskID: "task-G", certs: certs})

	deadline := time.Now().Add(3 * time.Second)
	for {
		h.mu.Lock()
		flushed := h.certCount
		h.mu.Unlock()
		if flushed == 10 {
			break
		}
		if time.Now().After(deadline) {
			w.Stop()
			t.Fatalf("quantitative flush did not fire, certCount=%d", flushed)
		}
		time.Sleep(20 * time.Millisecond)
	}
	w.Stop()
}

// TestAsyncResultWriter_ConcurrentEnqueue 并发投递不丢数据（通道容量充足时全量落库）
func TestAsyncResultWriter_ConcurrentEnqueue(t *testing.T) {
	h := &asyncWriterTestHarness{}
	w := newTestAsyncWriter(h, AsyncWriterConfig{
		ChanSize:       4096,
		FlushInterval:  50 * time.Millisecond,
		FlushTimeout:   5 * time.Second,
		MaxAssetsBatch: 50,
		MaxOtherBatch:  100,
	})
	w.Start()

	var sent int64
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				if w.Enqueue(&asyncWriteRequest{
					kind:       asyncWriteAssets,
					mainTaskID: "task-H",
					orgID:      "o1",
					assets:     []*scanner.Asset{{Host: "h.example.com", Port: 80}},
				}) {
					atomic.AddInt64(&sent, 1)
				}
			}
		}()
	}
	wg.Wait()
	w.Stop()

	h.mu.Lock()
	defer h.mu.Unlock()
	total := 0
	for _, r := range h.assetRows {
		total += r.n
	}
	if int64(total) != atomic.LoadInt64(&sent) {
		t.Fatalf("flushed %d assets but %d were accepted by enqueue", total, atomic.LoadInt64(&sent))
	}
	if total != 400 {
		t.Fatalf("expected 400 assets flushed (chan large enough), got %d", total)
	}
}
