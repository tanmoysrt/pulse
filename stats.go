package main

import (
	"bufio"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// A sample is one instant of machine load. cpu/mem are percentages; rx/tx are
// bytes per second across all real interfaces.
type sample struct {
	T   int64   `json:"t"`
	CPU float64 `json:"cpu"`
	Mem float64 `json:"mem"`
	Rx  float64 `json:"rx"`
	Tx  float64 `json:"tx"`
}

const (
	statsInterval = 5 * time.Second
	statsSlots    = 60 // 5s × 60 = last 5 minutes
)

// statsRing is a fixed-size circular buffer of the last statsSlots samples — the
// ring keeps memory flat while always holding the most recent window.
type statsRing struct {
	mu   sync.Mutex
	buf  [statsSlots]sample
	head int // next write position
	n    int // filled slots (<= statsSlots)
}

func newStatsRing() *statsRing { return &statsRing{} }

func (r *statsRing) add(s sample) {
	r.mu.Lock()
	r.buf[r.head] = s
	r.head = (r.head + 1) % statsSlots
	if r.n < statsSlots {
		r.n++
	}
	r.mu.Unlock()
}

// snapshot returns the samples oldest-first.
func (r *statsRing) snapshot() []sample {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]sample, 0, r.n)
	start := (r.head - r.n + statsSlots) % statsSlots
	for i := 0; i < r.n; i++ {
		out = append(out, r.buf[(start+i)%statsSlots])
	}
	return out
}

// collect samples machine load every statsInterval for the life of the daemon.
// On platforms without /proc it simply records nothing.
func (r *statsRing) collect() {
	prevTotal, prevIdle := readCPU()
	prevRx, prevTx := readNet()
	last := time.Now()
	for range time.Tick(statsInterval) {
		now := time.Now()
		secs := now.Sub(last).Seconds()
		last = now

		total, idle := readCPU()
		cpu := 0.0
		if dt := total - prevTotal; dt > 0 {
			cpu = 100 * float64((total-prevTotal)-(idle-prevIdle)) / float64(dt)
		}
		prevTotal, prevIdle = total, idle

		rxNow, txNow := readNet()
		rx, tx := 0.0, 0.0
		if secs > 0 {
			rx = float64(rxNow-prevRx) / secs
			tx = float64(txNow-prevTx) / secs
		}
		prevRx, prevTx = rxNow, txNow

		r.add(sample{
			T:   now.UnixMilli(),
			CPU: clampPct(cpu),
			Mem: clampPct(readMem()),
			Rx:  maxF(rx, 0),
			Tx:  maxF(tx, 0),
		})
	}
}

func (d *Daemon) apiStats(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]any{
		"samples":  d.stats.snapshot(),
		"interval": int(statsInterval.Seconds()),
	})
}

// readCPU returns cumulative total and idle jiffies from /proc/stat.
func readCPU() (total, idle uint64) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return 0, 0
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0
	}
	for i, v := range fields[1:] {
		n, _ := strconv.ParseUint(v, 10, 64)
		total += n
		if i == 3 || i == 4 { // idle + iowait
			idle += n
		}
	}
	return total, idle
}

// readMem returns used memory as a percentage of total (/proc/meminfo).
func readMem() float64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	var total, avail float64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		v, _ := strconv.ParseFloat(fields[1], 64)
		switch fields[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			avail = v
		}
	}
	if sc.Err() != nil {
		return 0
	}
	if total == 0 {
		return 0
	}
	return 100 * (total - avail) / total
}

// readNet returns total received and transmitted bytes across real interfaces
// (loopback and virtual bridges excluded) from /proc/net/dev.
func readNet() (rx, tx uint64) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		colon := strings.IndexByte(line, ':')
		if colon < 0 {
			continue
		}
		name := strings.TrimSpace(line[:colon])
		if name == "lo" || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "br-") {
			continue
		}
		fields := strings.Fields(line[colon+1:])
		if len(fields) < 9 {
			continue
		}
		r, _ := strconv.ParseUint(fields[0], 10, 64)
		t, _ := strconv.ParseUint(fields[8], 10, 64)
		rx += r
		tx += t
	}
	if sc.Err() != nil {
		return 0, 0
	}
	return rx, tx
}

func clampPct(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
