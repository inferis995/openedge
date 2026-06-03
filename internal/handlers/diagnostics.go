// Package handlers — diagnostics.
//
// Exposes /api/system/diagnostics for the operator-facing "is the box
// healthy?" page. Read-only; reads /proc + /sys (host paths inside the
// container) so it works without extra agents. On Windows / non-Linux
// the same endpoint returns "unsupported" for the host-only fields and
// keeps the Docker/Postgres/Redis checks (which work everywhere).
package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/ralph/industrial-edge-middleware/internal/redis"
)

// DiagnosticsHandler exposes hardware + service health to the UI.
type DiagnosticsHandler struct {
	db          *sql.DB
	redisClient *redis.Client
	startedAt   time.Time
}

func NewDiagnosticsHandler(db *sql.DB, r *redis.Client) *DiagnosticsHandler {
	return &DiagnosticsHandler{db: db, redisClient: r, startedAt: time.Now()}
}

// DiagnosticsResponse is the shape consumed by the UI. Every section is
// optional — failures inside the handler degrade gracefully so the page
// still renders the parts that DID resolve.
type DiagnosticsResponse struct {
	GeneratedAt time.Time           `json:"generated_at"`
	Host        HostInfo            `json:"host"`
	CPU         CPUInfo             `json:"cpu,omitempty"`
	Memory      MemoryInfo          `json:"memory,omitempty"`
	Disk        []DiskInfo          `json:"disk,omitempty"`
	Network     []NetworkIfInfo     `json:"network,omitempty"`
	Services    map[string]Health   `json:"services"` // postgres / redis / api
}

type HostInfo struct {
	Hostname     string        `json:"hostname"`
	OS           string        `json:"os"`
	Kernel       string        `json:"kernel,omitempty"`
	Arch         string        `json:"arch"`
	UptimeSec    int64         `json:"uptime_sec,omitempty"`     // host uptime when available
	APIUptimeSec int64         `json:"api_uptime_sec"`           // this process
	LoadAverage  []float64     `json:"load_average,omitempty"`   // 1/5/15 min
	NowUTC       time.Time     `json:"now_utc"`
}

type CPUInfo struct {
	Cores     int      `json:"cores"`
	ModelName string   `json:"model_name,omitempty"`
	UsagePct  *float64 `json:"usage_pct,omitempty"` // best-effort, may be nil
}

type MemoryInfo struct {
	TotalBytes     uint64  `json:"total_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsedPct        float64 `json:"used_pct"`
}

type DiskInfo struct {
	Path          string  `json:"path"`
	Filesystem    string  `json:"filesystem,omitempty"`
	TotalBytes    uint64  `json:"total_bytes"`
	UsedBytes     uint64  `json:"used_bytes"`
	UsedPct       float64 `json:"used_pct"`
}

type NetworkIfInfo struct {
	Name       string `json:"name"`
	LinkUp     bool   `json:"link_up"`
	RxBytes    uint64 `json:"rx_bytes"`
	TxBytes    uint64 `json:"tx_bytes"`
	RxErrors   uint64 `json:"rx_errors"`
	TxErrors   uint64 `json:"tx_errors"`
}

type Health struct {
	OK       bool   `json:"ok"`
	Message  string `json:"message,omitempty"`
	LatencyMs int64 `json:"latency_ms,omitempty"`
}

// Get returns a single diagnostics snapshot. Cheap — under 100 µs of
// CPU on a modern box even with all sections enabled.
func (h *DiagnosticsHandler) Get(c *gin.Context) {
	resp := DiagnosticsResponse{
		GeneratedAt: time.Now().UTC(),
		Host:        h.collectHost(),
		Services:    map[string]Health{},
	}
	resp.Services["postgres"] = h.checkPostgres()
	resp.Services["redis"] = h.checkRedis()
	resp.Services["api"] = Health{OK: true, Message: "running"}

	if runtime.GOOS == "linux" {
		resp.CPU = collectCPULinux()
		resp.Memory = collectMemLinux()
		resp.Disk = collectDiskLinux([]string{"/", "/backups", "/data"})
		resp.Network = collectNetLinux()
	} else {
		// Minimal fallback so the UI still has SOMETHING to show.
		resp.CPU = CPUInfo{Cores: runtime.NumCPU()}
	}

	c.JSON(http.StatusOK, resp)
}

func (h *DiagnosticsHandler) collectHost() HostInfo {
	host, _ := os.Hostname()
	info := HostInfo{
		Hostname:     host,
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		APIUptimeSec: int64(time.Since(h.startedAt).Seconds()),
		NowUTC:       time.Now().UTC(),
	}
	if runtime.GOOS == "linux" {
		info.Kernel = readKernel()
		if u := readHostUptime(); u > 0 {
			info.UptimeSec = u
		}
		info.LoadAverage = readLoadAvg()
	}
	return info
}

func (h *DiagnosticsHandler) checkPostgres() Health {
	if h.db == nil {
		return Health{OK: false, Message: "no db handle"}
	}
	ctx, cancel := context.WithTimeout(c2x(), 2*time.Second)
	defer cancel()
	start := time.Now()
	if err := h.db.PingContext(ctx); err != nil {
		return Health{OK: false, Message: err.Error()}
	}
	return Health{OK: true, LatencyMs: time.Since(start).Milliseconds()}
}

func (h *DiagnosticsHandler) checkRedis() Health {
	if h.redisClient == nil {
		return Health{OK: false, Message: "no redis handle"}
	}
	start := time.Now()
	if _, err := h.redisClient.Get("__diag_probe__"); err != nil && !isRedisNotFound(err) {
		return Health{OK: false, Message: err.Error()}
	}
	return Health{OK: true, LatencyMs: time.Since(start).Milliseconds()}
}

// isRedisNotFound treats a missing-key error as a healthy round-trip —
// the probe key never exists, the connection succeeded, which is what
// "OK" should mean here. Matches both the upstream go-redis "nil" reply
// and our wrapper's friendlier "key X does not exist" message
// (internal/redis/client.go:79).
func isRedisNotFound(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "nil") || strings.Contains(s, "does not exist")
}

// c2x returns a background context. Tiny helper kept private so we
// don't import context at the call site twice — there is only one
// timeout path in this handler.
func c2x() context.Context { return context.Background() }

// ── Linux-only collectors (read /proc, /sys) ───────────────────────────────

func readKernel() string {
	if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		return strings.TrimSpace(string(b))
	}
	return ""
}

func readHostUptime() int64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	parts := strings.Fields(string(b))
	if len(parts) == 0 {
		return 0
	}
	f, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0
	}
	return int64(f)
}

func readLoadAvg() []float64 {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil
	}
	parts := strings.Fields(string(b))
	if len(parts) < 3 {
		return nil
	}
	out := make([]float64, 0, 3)
	for i := 0; i < 3; i++ {
		v, err := strconv.ParseFloat(parts[i], 64)
		if err != nil {
			return nil
		}
		out = append(out, v)
	}
	return out
}

func collectCPULinux() CPUInfo {
	info := CPUInfo{Cores: runtime.NumCPU()}
	b, err := os.ReadFile("/proc/cpuinfo")
	if err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "model name") {
				if idx := strings.Index(line, ":"); idx >= 0 {
					info.ModelName = strings.TrimSpace(line[idx+1:])
					break
				}
			}
		}
	}
	if pct, ok := sampleCPUUsage(); ok {
		info.UsagePct = &pct
	}
	return info
}

// sampleCPUUsage takes a quick before/after read of /proc/stat with a
// 100 ms delay and reports the % of CPU spent in non-idle states across
// all cores. Best-effort; on read errors we return (0, false) and the
// UI shows "—" rather than a fake 0.
func sampleCPUUsage() (float64, bool) {
	t1, i1, ok1 := readCPUStat()
	if !ok1 {
		return 0, false
	}
	time.Sleep(100 * time.Millisecond)
	t2, i2, ok2 := readCPUStat()
	if !ok2 || t2 <= t1 {
		return 0, false
	}
	total := float64(t2 - t1)
	idle := float64(i2 - i1)
	return 100 * (1 - idle/total), true
}

func readCPUStat() (total, idle uint64, ok bool) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		// fields = ["cpu", user, nice, system, idle, iowait, irq, softirq, steal, guest, guest_nice]
		var sum uint64
		for i := 1; i < len(fields); i++ {
			v, err := strconv.ParseUint(fields[i], 10, 64)
			if err != nil {
				return 0, 0, false
			}
			sum += v
			if i == 4 {
				idle = v
			}
		}
		return sum, idle, true
	}
	return 0, 0, false
}

func collectMemLinux() MemoryInfo {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return MemoryInfo{}
	}
	var memTotal, memAvail uint64
	for _, line := range strings.Split(string(b), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		v, _ := strconv.ParseUint(parts[1], 10, 64)
		v *= 1024 // kB → bytes
		switch parts[0] {
		case "MemTotal:":
			memTotal = v
		case "MemAvailable:":
			memAvail = v
		}
	}
	if memTotal == 0 {
		return MemoryInfo{}
	}
	used := memTotal - memAvail
	return MemoryInfo{
		TotalBytes:     memTotal,
		AvailableBytes: memAvail,
		UsedPct:        float64(used) / float64(memTotal) * 100,
	}
}

// collectDiskLinux probes a fixed set of mount points using statfs.
// We don't enumerate every mount: an industrial PC has 1-2 disks of
// interest (the root, optionally /data for the DB volume) and reading
// the whole table is noise.
func collectDiskLinux(paths []string) []DiskInfo {
	out := []DiskInfo{}
	for _, p := range paths {
		di, ok := statfsBytes(p)
		if !ok {
			continue
		}
		di.Path = p
		out = append(out, di)
	}
	return out
}

func collectNetLinux() []NetworkIfInfo {
	b, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return nil
	}
	out := []NetworkIfInfo{}
	for _, line := range strings.Split(string(b), "\n") {
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		name := strings.TrimSpace(line[:idx])
		// Skip loopback, docker bridges, veth pairs — they're noise on
		// the diagnostics page and the operator doesn't manage them.
		if name == "lo" || strings.HasPrefix(name, "docker") ||
			strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "br-") {
			continue
		}
		fields := strings.Fields(line[idx+1:])
		if len(fields) < 16 {
			continue
		}
		rx, _ := strconv.ParseUint(fields[0], 10, 64)
		rxErr, _ := strconv.ParseUint(fields[2], 10, 64)
		tx, _ := strconv.ParseUint(fields[8], 10, 64)
		txErr, _ := strconv.ParseUint(fields[10], 10, 64)
		linkUp := false
		if op, err := os.ReadFile(fmt.Sprintf("/sys/class/net/%s/operstate", name)); err == nil {
			linkUp = strings.TrimSpace(string(op)) == "up"
		}
		out = append(out, NetworkIfInfo{
			Name: name, LinkUp: linkUp,
			RxBytes: rx, TxBytes: tx, RxErrors: rxErr, TxErrors: txErr,
		})
	}
	return out
}
