package main

import (
	"bytes"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

var (
	listenAddr   = getenv("LISTEN_ADDR", ":8080")
	khajuScripts = getenv("KHAJUBRIDGE_SCRIPTS", "/opt/khajubridge/scripts")
	conduitUnit  = getenv("CONDUIT_UNIT", "conduit.service")
	journalTail  = getenv("JOURNAL_TAIL", "200")
	allowCIDRs   = getenv("ALLOW_CIDRS", "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,127.0.0.1/32")
)

func run(cmd string, args ...string) (string, int) {
	c := exec.Command(cmd, args...)
	var out, errb bytes.Buffer
	c.Stdout = &out
	c.Stderr = &errb
	err := c.Run()

	code := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			code = 1
		}
	}
	s := strings.TrimSpace(out.String())
	e := strings.TrimSpace(errb.String())
	if e != "" {
		if s != "" {
			s += "\n"
		}
		s += e
	}
	return s, code
}

type ConduitStats struct {
	Connecting int
	Connected  int
	UpTotal    string
	DownTotal  string
	UpBps      string
	DownBps    string
}

func readConduitStatsFromJournal() ConduitStats {
	// Uses allowed sudo rule for journalctl on the Conduit unit
	out, _ := run("sudo", "-n", "/bin/journalctl", "-u", conduitUnit, "-n", journalTail, "--no-pager")
	lines := strings.Split(out, "\n")

	// Collect last 2 [STATS] lines
	statsLines := make([]string, 0, 2)
	for i := len(lines) - 1; i >= 0 && len(statsLines) < 2; i-- {
		if strings.Contains(lines[i], "[STATS]") {
			statsLines = append(statsLines, lines[i])
		}
	}
	if len(statsLines) == 0 {
		return ConduitStats{UpBps: "-", DownBps: "-", UpTotal: "-", DownTotal: "-"}
	}
	// reverse so statsLines[0] is older, [1] is newer when we have 2
	if len(statsLines) == 2 {
		statsLines[0], statsLines[1] = statsLines[1], statsLines[0]
	}

	parse := func(line string) (ts time.Time, connecting, connected int, upStr, downStr string, upBytes, downBytes float64) {
		// Example: 2026-01-31 05:33:39 [STATS] Connecting: 4 | Connected: 19 | Up: 950.0 MB | Down: 8.7 GB | Uptime: ...
		// Extract the ISO-ish timestamp at the start
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			// Parse: YYYY-MM-DD HH:MM:SS
			ts, _ = time.Parse("2006-01-02 15:04:05", fields[0]+" "+fields[1])
		}
		idx := strings.Index(line, "[STATS]")
		if idx >= 0 {
			line = strings.TrimSpace(line[idx+len("[STATS]"):])
		}
		parts := strings.Split(line, "|")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			switch {
			case strings.HasPrefix(p, "Connecting:"):
				fmt.Sscanf(p, "Connecting: %d", &connecting)
			case strings.HasPrefix(p, "Connected:"):
				fmt.Sscanf(p, "Connected: %d", &connected)
			case strings.HasPrefix(p, "Up:"):
				upStr = strings.TrimSpace(strings.TrimPrefix(p, "Up:"))
				upBytes = parseSizeToBytes(upStr)
			case strings.HasPrefix(p, "Down:"):
				downStr = strings.TrimSpace(strings.TrimPrefix(p, "Down:"))
				downBytes = parseSizeToBytes(downStr)
			}
		}
		return
	}

	// Newest line is always statsLines[len-1]
	var s ConduitStats
	if len(statsLines) == 1 {
		ts2, c1, c2, upT, downT, _, _ := parse(statsLines[0])
		_ = ts2
		s.Connecting = c1
		s.Connected = c2
		s.UpTotal = upT
		s.DownTotal = downT
		s.UpBps = "-"
		s.DownBps = "-"
		return s
	}

	ts1, _, _, _, _, up1, down1 := parse(statsLines[0])
	ts2, conn2, con2, upT2, downT2, up2, down2 := parse(statsLines[1])
	dt := ts2.Sub(ts1).Seconds()
	if dt <= 0 {
		dt = 1
	}
	s.Connecting = conn2
	s.Connected = con2
	s.UpTotal = upT2
	s.DownTotal = downT2

	upRate := (up2 - up1) / dt
	downRate := (down2 - down1) / dt
	s.UpBps = formatBytesPerSec(upRate)
	s.DownBps = formatBytesPerSec(downRate)
	return s
}

func parseSizeToBytes(s string) float64 {
	// accepts values like: "950.0 MB", "1.0 GB", "1023.3 MB"
	s = strings.TrimSpace(s)
	var v float64
	var unit string
	fmt.Sscanf(s, "%f %s", &v, &unit)
	unit = strings.ToUpper(strings.TrimSpace(unit))
	switch unit {
	case "B":
		return v
	case "KB":
		return v * 1024
	case "MB":
		return v * 1024 * 1024
	case "GB":
		return v * 1024 * 1024 * 1024
	case "TB":
		return v * 1024 * 1024 * 1024 * 1024
	default:
		return v
	}
}

func formatBytesPerSec(bps float64) string {
	if bps < 0 {
		bps = 0
	}
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case bps >= GB:
		return fmt.Sprintf("%.2f GB/s", bps/GB)
	case bps >= MB:
		return fmt.Sprintf("%.2f MB/s", bps/MB)
	case bps >= KB:
		return fmt.Sprintf("%.1f KB/s", bps/KB)
	default:
		return fmt.Sprintf("%.0f B/s", bps)
	}
}

func isLAN(ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}
	private := strings.Split(allowCIDRs, ",")
	for _, c := range private {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		_, b, err := net.ParseCIDR(c)
		if err == nil && b.Contains(ip) {
			return true
		}
	}
	return false
}

func lanOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		ip := net.ParseIP(host)
		if ip == nil || !isLAN(ip) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

var page = template.Must(template.New("page").Parse(`
<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>KhajunBridge</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<script src="https://unpkg.com/htmx.org@1.9.12"></script>

<style>
:root{
 --bg:#0b0f14;--panel:#111826cc;--panel2:#0f1623cc;--border:#243041;
 --text:#e7eef8;--muted:#9db0c7;--good:#37d67a;
 --shadow:0 18px 40px rgba(0,0,0,.45);--r:16px;
}
*{box-sizing:border-box}
body{
 margin:0;font-family:system-ui;background:
 radial-gradient(1000px 500px at 20% 0%,rgba(77,141,255,.18),transparent 60%),
 radial-gradient(900px 450px at 85% 20%,rgba(120,80,255,.14),transparent 55%),
 var(--bg);
 color:var(--text)
}
.app{display:grid;grid-template-columns:320px 1fr;gap:16px;padding:16px;min-height:100vh}
.sidebar,.main{
 background:linear-gradient(180deg,var(--panel),var(--panel2));
 border:1px solid var(--border);border-radius:var(--r);
 box-shadow:var(--shadow)
}
.sidebar{padding:16px;display:flex;flex-direction:column;min-height:100vh}.main{padding:16px}
.title{display:flex;justify-content:space-between;align-items:center;
 padding:10px 12px;border:1px solid var(--border);border-radius:14px;
 background:rgba(0,0,0,.18);margin-bottom:14px}
h1{font-size:18px;margin:0}
.pill{display:inline-flex;gap:8px;align-items:center;
 padding:6px 10px;border-radius:999px;border:1px solid var(--border);
 background:rgba(0,0,0,.25);font-size:12px;color:var(--muted)}
.dot{width:10px;height:10px;border-radius:50%}
.dot.good{background:var(--good)}
.section{color:var(--muted);font-size:12px;letter-spacing:.12em;
 text-transform:uppercase;margin:14px 0 10px}
.btnRow{display:flex;gap:10px;flex-wrap:wrap}
button{
 border:1px solid var(--border);background:rgba(0,0,0,.28);
 color:var(--text);padding:10px 12px;border-radius:14px;cursor:pointer
}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:12px}
.card{
 border:1px solid var(--border);background:rgba(0,0,0,.25);
 border-radius:var(--r);padding:12px 14px
}
.k{color:var(--muted);font-size:12px}
 .tagline{margin-top:6px;color:rgba(231,238,248,.72);font-size:12px;line-height:1.35;max-width:52ch}
 .sidebarFooter{margin-top:auto;padding-top:14px;color:rgba(231,238,248,.55);font-size:12px}
 .sfTitle{letter-spacing:.02em;margin-bottom:6px}
 .sfLink{color:rgba(157,176,199,.85);text-decoration:none;font-size:12px}
 .sfLink:hover{color:rgba(231,238,248,.9);text-decoration:underline}
.v{font-size:20px;font-weight:650;margin-top:4px}
.logs{margin-top:14px;border:1px solid var(--border);
 background:rgba(0,0,0,.35);border-radius:var(--r);overflow:hidden}
.logsHead{display:flex;justify-content:space-between;
 padding:10px 12px;border-bottom:1px solid var(--border)}
pre{margin:0;padding:12px;font-size:12px;color:#b9f6c6;
 font-family:ui-monospace,monospace;white-space:pre-wrap}
@media(max-width:900px){.app{grid-template-columns:1fr}}
</style>
</head>

<body>
<div class="app">
<aside class="sidebar">
 <div class="title">
  <h1>KhajunBridge</h1>
  <span class="pill"><span class="dot good"></span>Running</span>
 </div>

 <div class="card" hx-get="/status" hx-trigger="load, every 5s">
  Loading status…
 </div>

 <div class="section">Actions</div>
 <div class="btnRow">
  <button hx-post="/action/update-cidrs" hx-target="#logbox">Update CIDRs</button>
  <button hx-post="/action/apply" hx-target="#logbox">Apply Firewall</button>
  <button hx-get="/logs" hx-target="#logbox">Show Logs</button>
 </div>

  <div class="sidebarFooter">
   <div class="sfTitle">KhajuBridge · Open source</div>
   <a class="sfLink" href="https://github.com/delejos/KhajuBridge" target="_blank" rel="noopener">github.com/delejos/KhajuBridge</a>
  </div>

</aside>

<main class="main">
 <div class="title">
  <div>
   <div class="k">System Overview</div>
   <div class="k">KhajuBridge + Conduit</div>
     <div class="tagline">Powered by Conduit — built with difficult networks in mind.</div>
  </div>
 </div>

 <div id="overview" hx-get="/overview" hx-trigger="load, every 5s">
  Loading overview…
 </div>

 <div class="logs">
  <div class="logsHead">
   <b>System Logs</b>
  </div>
  <pre id="logbox" hx-get="/logs" hx-trigger="load" style="max-height:180px; overflow:auto; margin:0; padding:12px;">Loading logs…</pre>
 </div>
</main>
</div>
</body>
</html>
`))

func statusHandler(w http.ResponseWriter, r *http.Request) {
	_, c := run("systemctl", "is-active", conduitUnit)
	state := "inactive"
	if c == 0 {
		state = "active"
	}
	fmt.Fprintf(w, "<b>Conduit:</b> %s", state)
}

func overviewHandler(w http.ResponseWriter, r *http.Request) {
	uptime := readUptime()
	cpu := readCPULoadPct()
	used, total := readMemMB()
	cs := readConduitStatsFromJournal()

	fmt.Fprintf(w, `
<div class="grid">
 <div class="card"><div class="k">Uptime</div><div class="v">%s</div></div>
 <div class="card"><div class="k">CPU</div><div class="v">%.1f%%</div></div>
 <div class="card"><div class="k">RAM</div><div class="v">%d / %d MiB</div></div>
</div>

  <div class="grid">
   <div class="card"><div class="k">Active Users</div><div class="v">%d</div></div>
   <div class="card"><div class="k">Connecting</div><div class="v">%d</div></div>
   <div class="card"><div class="k">Up Speed</div><div class="v">%s</div></div>
   <div class="card"><div class="k">Down Speed</div><div class="v">%s</div></div>
   <div class="card"><div class="k">Total Upload</div><div class="v">%s</div></div>
     <div class="card"><div class="k">Total Download</div><div class="v">%s</div></div>
  </div>

`, uptime, cpu, used, total, cs.Connected, cs.Connecting, cs.UpBps, cs.DownBps, cs.UpTotal, cs.DownTotal)
}

func actionUpdateCIDRs(w http.ResponseWriter, r *http.Request) {
	out, _ := run("sudo", khajuScripts+"/update_region_cidrs.sh")
	fmt.Fprint(w, out)
}

func actionApply(w http.ResponseWriter, r *http.Request) {
	out, _ := run("sudo", khajuScripts+"/apply_firewall.sh")
	fmt.Fprint(w, out)
}

func logsHandler(w http.ResponseWriter, r *http.Request) {
	out, _ := run("sudo", "-n", "/bin/journalctl", "-u", conduitUnit, "-n", journalTail, "--no-pager")
	fmt.Fprint(w, out)
}

func readUptime() string {
	b, _ := os.ReadFile("/proc/uptime")
	sec, _ := strconv.ParseFloat(strings.Fields(string(b))[0], 64)
	d := time.Duration(sec * float64(time.Second))
	return d.Truncate(time.Minute).String()
}

func readMemMB() (int, int) {
	b, _ := os.ReadFile("/proc/meminfo")
	var t, a int
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "MemTotal:") {
			fmt.Sscanf(l, "MemTotal: %d kB", &t)
		}
		if strings.HasPrefix(l, "MemAvailable:") {
			fmt.Sscanf(l, "MemAvailable: %d kB", &a)
		}
	}
	return (t - a) / 1024, t / 1024
}

func readCPULoadPct() float64 {
	aIdle, aTotal := cpuTimes()
	time.Sleep(250 * time.Millisecond)
	bIdle, bTotal := cpuTimes()
	return (1 - float64(bIdle-aIdle)/float64(bTotal-aTotal)) * 100
}

func cpuTimes() (idle, total uint64) {
	b, _ := os.ReadFile("/proc/stat")
	var u, n, s, i, io, irq, sirq, st uint64
	fmt.Sscanf(strings.Split(string(b), "\n")[0],
		"cpu  %d %d %d %d %d %d %d %d",
		&u, &n, &s, &i, &io, &irq, &sirq, &st)
	total = u + n + s + i + io + irq + sirq + st
	return i + io, total
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_ = page.Execute(w, nil)
	})
	mux.HandleFunc("/status", statusHandler)
	mux.HandleFunc("/overview", overviewHandler)
	mux.HandleFunc("/action/update-cidrs", actionUpdateCIDRs)
	mux.HandleFunc("/action/apply", actionApply)
	mux.HandleFunc("/logs", logsHandler)

	fmt.Println("Listening on", listenAddr)
	http.ListenAndServe(listenAddr, lanOnly(mux))
}
