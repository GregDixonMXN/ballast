// Package telemetry isolates observability wiring: structured logging with
// domain IDs plus counters/timings. OTEL SDK export lives here behind a
// no-op-safe API so instrumentation points never change when we add a
// collector. MVP exports human-readable logs to stdout.
package telemetry

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"
)

var (
	logger *slog.Logger
	once   sync.Once
)

// Init configures the global structured logger.
func Init(service string) {
	once.Do(func() {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{})).
			With("service", service)
	})
}

func L() *slog.Logger {
	if logger == nil {
		Init("ballast")
	}
	return logger
}

// Ctx carries trace + domain IDs through API → runner → git → tests.
type Ctx struct {
	TraceID     string
	OrgID       string
	ProjectID   string
	TaskID      string
	WorkspaceID string
	AgentID     string
	RunnerID    string
}

type ctxKey struct{}

// With returns a context carrying telemetry IDs.
func With(ctx context.Context, c Ctx) context.Context {
	return context.WithValue(ctx, ctxKey{}, c)
}

// From extracts IDs (zero value when absent).
func From(ctx context.Context) Ctx {
	c, _ := ctx.Value(ctxKey{}).(Ctx)
	return c
}

// Log emits a structured record with context IDs merged in.
func Log(ctx context.Context, msg string, args ...any) {
	c := From(ctx)
	base := []any{
		"trace_id", c.TraceID,
		"org", c.OrgID, "project", c.ProjectID, "task", c.TaskID,
		"workspace", c.WorkspaceID, "agent", c.AgentID, "runner", c.RunnerID,
	}
	L().Info(msg, append(base, args...)...)
}

// Counters is a minimal in-process metric registry (OTEL-ready names).
type Counters struct {
	mu sync.Mutex
	m  map[string]int64
	d  map[string][]float64
}

func NewCounters() *Counters { return &Counters{m: map[string]int64{}, d: map[string][]float64{}} }

func (c *Counters) Inc(name string) {
	c.mu.Lock()
	c.m[name]++
	c.mu.Unlock()
}

func (c *Counters) Observe(name string, seconds float64) {
	c.mu.Lock()
	c.d[name] = append(c.d[name], seconds)
	c.mu.Unlock()
}

func (c *Counters) Snapshot() (map[string]int64, map[string]float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	counts := map[string]int64{}
	means := map[string]float64{}
	for k, v := range c.m {
		counts[k] = v
	}
	for k, vs := range c.d {
		var s float64
		for _, v := range vs {
			s += v
		}
		if len(vs) > 0 {
			means[k] = s / float64(len(vs))
		}
	}
	return counts, means
}

// Timed measures fn and records under name.
func (c *Counters) Timed(name string, fn func()) {
	t := time.Now()
	fn()
	c.Observe(name+"_duration_seconds", time.Since(t).Seconds())
}
