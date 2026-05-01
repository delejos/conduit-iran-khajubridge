package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	stateFile    = getenv("STATE_FILE", "/etc/khajubridge/state.json")
	trafficStats = getenv("TRAFFIC_STATS", "/opt/conduit/traffic_stats/cumulative_data")
)

// ── Cache ─────────────────────────────────────────────────────────────────────

type cacheEntry struct {
	value interface{}
	ts    time.Time
}

var (
	cacheMu sync.Mutex
	cache   = map[string]cacheEntry{}
)

func cached(key string, ttl time.Duration, fn func() interface{}) interface{} {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if e, ok := cache[key]; ok && time.Since(e.ts) < ttl {
		return e.value
	}
	v := fn()
	cache[key] = cacheEntry{value: v, ts: time.Now()}
	return v
}

// ── Shell helpers ─────────────────────────────────────────────────────────────

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
	if e := strings.TrimSpace(errb.String()); e != "" {
		if s != "" {
			s += "\n"
		}
		s += e
	}
	return s, code
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

func stripAnsi(s string) string {
	return ansiRe.ReplaceAllString(s, "")
}

// ── System stats ──────────────────────────────────────────────────────────────

func readUptime() string {
	b, _ := os.ReadFile("/proc/uptime")
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return "-"
	}
	sec, _ := strconv.ParseFloat(fields[0], 64)
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
	a1, t1 := cpuTimes()
	time.Sleep(200 * time.Millisecond)
	a2, t2 := cpuTimes()
	if t2 == t1 {
		return 0
	}
	return (1 - float64(a2-a1)/float64(t2-t1)) * 100
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

// ── Conduit stats from journal ────────────────────────────────────────────────

type ConduitStats struct {
	Connecting int
	Connected  int
	UpTotal    string
	DownTotal  string
	UpBps      string
	DownBps    string
}

func readConduitStats() ConduitStats {
	v := cached("conduit_stats", 8*time.Second, func() interface{} {
		return fetchConduitStats()
	})
	return v.(ConduitStats)
}

func fetchConduitStats() ConduitStats {
	out, _ := run("sudo", "-n", "/bin/journalctl", "-u", conduitUnit, "-n", journalTail, "--no-pager")
	out = stripAnsi(out)
	lines := strings.Split(out, "\n")

	var statsLines []string
	for i := len(lines) - 1; i >= 0 && len(statsLines) < 2; i-- {
		if strings.Contains(lines[i], "[STATS]") {
			statsLines = append(statsLines, lines[i])
		}
	}
	if len(statsLines) == 0 {
		return ConduitStats{UpBps: "-", DownBps: "-", UpTotal: "-", DownTotal: "-"}
	}
	if len(statsLines) == 2 {
		statsLines[0], statsLines[1] = statsLines[1], statsLines[0]
	}

	parse := func(line string) (ts time.Time, connecting, connected int, upStr, downStr string, upBytes, downBytes float64) {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			ts, _ = time.Parse("2006-01-02 15:04:05", fields[0]+" "+fields[1])
		}
		idx := strings.Index(line, "[STATS]")
		if idx >= 0 {
			line = strings.TrimSpace(line[idx+len("[STATS]"):])
		}
		for _, p := range strings.Split(line, "|") {
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

	var s ConduitStats
	if len(statsLines) == 1 {
		_, c1, c2, upT, downT, _, _ := parse(statsLines[0])
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
	s.UpBps = formatBps((up2 - up1) / dt)
	s.DownBps = formatBps((down2 - down1) / dt)
	return s
}

func parseSizeToBytes(s string) float64 {
	s = strings.TrimSpace(s)
	var v float64
	var unit string
	fmt.Sscanf(s, "%f %s", &v, &unit)
	switch strings.ToUpper(strings.TrimSpace(unit)) {
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

func formatBps(bps float64) string {
	if bps < 0 {
		bps = 0
	}
	const KB, MB, GB = 1024.0, 1024 * 1024.0, 1024 * 1024 * 1024.0
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

// ── Assurance — reads state.json written by apply_firewall.sh ─────────────────

type Assurance struct {
	EnforcementStatus string
	LastAppliedUTC    string
	RulesetHash       string
	CIDRSource        string
	V4Count           int
	V6Count           int
}

func getAssurance() Assurance {
	v := cached("assurance", 15*time.Second, func() interface{} {
		return fetchAssurance()
	})
	return v.(Assurance)
}

func fetchAssurance() Assurance {
	unknown := Assurance{
		EnforcementStatus: "UNKNOWN",
		LastAppliedUTC:    "Unknown",
		RulesetHash:       "Unknown",
		CIDRSource:        "Unknown",
	}

	// Verify the nftables table is actually loaded right now.
	_, code := run("nft", "list", "table", "inet", "khajubridge")
	if code != 0 {
		unknown.EnforcementStatus = "OFF"
		return unknown
	}

	// Read the state file written by apply_firewall.sh.
	data, err := os.ReadFile(stateFile)
	if err != nil {
		// Table is loaded but no state file — partially known.
		return Assurance{
			EnforcementStatus: "ON",
			LastAppliedUTC:    "Unknown (no state file)",
			RulesetHash:       "Unknown",
			CIDRSource:        "Unknown",
		}
	}

	var st struct {
		Status    string `json:"enforcement_status"`
		AppliedAt string `json:"applied_at_utc"`
		Hash      string `json:"ruleset_hash"`
		Source    string `json:"cidr_source"`
		V4Count   int    `json:"v4_count"`
		V6Count   int    `json:"v6_count"`
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return unknown
	}

	return Assurance{
		EnforcementStatus: st.Status,
		LastAppliedUTC:    st.AppliedAt,
		RulesetHash:       st.Hash,
		CIDRSource:        st.Source,
		V4Count:           st.V4Count,
		V6Count:           st.V6Count,
	}
}

func statusDotClass(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "ON", "ACTIVE", "ENABLED":
		return "good"
	case "OFF", "INACTIVE", "DISABLED":
		return "bad"
	default:
		return "unknown"
	}
}

// ── Country breakdown from traffic_stats/cumulative_data ──────────────────────
// File format: country|from_bytes|to_bytes  (borrowed from conduit-manager)

type CountryEntry struct {
	Country string
	Bytes   int64
	Total   string
}

type PeerStats struct {
	Inbound    []CountryEntry
	Outbound   []CountryEntry
	IranInPct  float64
	IranOutPct float64
	Available  bool
}

func getPeerStats() PeerStats {
	v := cached("peer_stats", 30*time.Second, func() interface{} {
		return fetchPeerStats()
	})
	return v.(PeerStats)
}

func fetchPeerStats() PeerStats {
	data, err := os.ReadFile(trafficStats)
	if err != nil {
		return PeerStats{}
	}

	var inbound, outbound []CountryEntry
	var inTotal, outTotal int64

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			continue
		}
		country := strings.TrimSpace(parts[0])
		fromBytes, _ := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		toBytes, _ := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
		if country == "" || strings.Contains(country, "error") {
			continue
		}
		if fromBytes > 0 {
			inbound = append(inbound, CountryEntry{Country: country, Bytes: fromBytes})
			inTotal += fromBytes
		}
		if toBytes > 0 {
			outbound = append(outbound, CountryEntry{Country: country, Bytes: toBytes})
			outTotal += toBytes
		}
	}

	sort.Slice(inbound, func(i, j int) bool { return inbound[i].Bytes > inbound[j].Bytes })
	sort.Slice(outbound, func(i, j int) bool { return outbound[i].Bytes > outbound[j].Bytes })

	format := func(b int64) string {
		switch {
		case b >= 1<<40:
			return fmt.Sprintf("%.2f TB", float64(b)/(1<<40))
		case b >= 1<<30:
			return fmt.Sprintf("%.2f GB", float64(b)/(1<<30))
		case b >= 1<<20:
			return fmt.Sprintf("%.2f MB", float64(b)/(1<<20))
		case b >= 1<<10:
			return fmt.Sprintf("%.2f KB", float64(b)/(1<<10))
		default:
			return fmt.Sprintf("%d B", b)
		}
	}

	top := func(entries []CountryEntry) []CountryEntry {
		if len(entries) > 10 {
			entries = entries[:10]
		}
		for i := range entries {
			entries[i].Total = format(entries[i].Bytes)
		}
		return entries
	}

	pct := func(entries []CountryEntry, total int64, country string) float64 {
		if total == 0 {
			return 0
		}
		for _, e := range entries {
			if strings.EqualFold(e.Country, country) || strings.EqualFold(e.Country, "IR") {
				return float64(e.Bytes) / float64(total) * 100
			}
		}
		return 0
	}

	return PeerStats{
		Inbound:    top(inbound),
		Outbound:   top(outbound),
		IranInPct:  pct(inbound, inTotal, "IR"),
		IranOutPct: pct(outbound, outTotal, "IR"),
		Available:  true,
	}
}

// ── LAN guard ─────────────────────────────────────────────────────────────────

func isLAN(ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}
	for _, c := range strings.Split(allowCIDRs, ",") {
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

// ── Page template ─────────────────────────────────────────────────────────────

var page = template.Must(template.New("page").Parse(`
<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>KhajuBridge</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<script src="https://unpkg.com/htmx.org@1.9.12"></script>

<style>
:root{
 --bg:#0b0f14;--panel:#111826cc;--panel2:#0f1623cc;--border:#243041;
 --text:#e7eef8;--muted:#9db0c7;--good:#37d67a;--bad:#e25555;--unk:#8a98ad;
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
.dot.bad{background:var(--bad)}
.dot.unknown{background:var(--unk)}
.section{color:var(--muted);font-size:12px;letter-spacing:.12em;
 text-transform:uppercase;margin:14px 0 10px}
.help{color:rgba(157,176,199,.75);font-size:12px;line-height:1.35;margin:-6px 0 10px}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(220px,1fr));gap:12px;margin-bottom:12px}
.card{border:1px solid var(--border);background:rgba(0,0,0,.25);border-radius:var(--r);padding:12px 14px}
.k{color:var(--muted);font-size:12px}
.v{font-size:20px;font-weight:650;margin-top:4px}
.tagline{margin-top:6px;color:rgba(231,238,248,.72);font-size:12px;line-height:1.35;max-width:52ch}
.sidebarFooter{margin-top:auto;padding-top:14px;color:rgba(231,238,248,.55);font-size:12px}
.sfTitle{letter-spacing:.02em;margin-bottom:6px}
.sfLink{color:rgba(157,176,199,.85);text-decoration:none;font-size:12px}
.sfLink:hover{color:rgba(231,238,248,.9);text-decoration:underline}
.logs{margin-top:14px;border:1px solid var(--border);background:rgba(0,0,0,.35);border-radius:var(--r);overflow:hidden}
.logsHead{display:flex;justify-content:space-between;padding:10px 12px;border-bottom:1px solid var(--border)}
pre{margin:0;padding:12px;font-size:12px;color:#b9f6c6;font-family:ui-monospace,monospace;white-space:pre-wrap}
.assurance{display:flex;flex-direction:column;gap:6px}
.kv{display:flex;justify-content:space-between;align-items:center;gap:12px;padding:6px 0;border-bottom:1px solid rgba(255,255,255,.06)}
.kv:last-child{border-bottom:none}
.kv .key{font-size:12px;opacity:.7}
.kv .val{font-size:12px;display:inline-flex;align-items:center;gap:8px;text-align:right}
.status{font-weight:650;letter-spacing:.2px}
.tbl{width:100%;border-collapse:collapse;font-size:12px}
.tbl th{color:var(--muted);font-weight:normal;text-align:left;padding:4px 6px;border-bottom:1px solid var(--border)}
.tbl td{padding:4px 6px;border-bottom:1px solid rgba(255,255,255,.04)}
.tbl tr:last-child td{border-bottom:none}
.iran{color:var(--good);font-weight:650}
.bar-wrap{background:rgba(255,255,255,.07);border-radius:4px;height:6px;min-width:60px}
.bar{background:var(--good);border-radius:4px;height:6px}
.btn{display:inline-flex;align-items:center;gap:6px;padding:6px 12px;border-radius:8px;
 border:1px solid var(--border);background:rgba(0,0,0,.3);color:var(--text);
 font-size:12px;cursor:pointer;text-decoration:none}
.btn:hover{background:rgba(255,255,255,.07)}
.btn-danger{border-color:#e25555;color:#e25555}
.btn-danger:hover{background:rgba(226,85,85,.1)}
@media(max-width:900px){.app{grid-template-columns:1fr}}
</style>
</head>
<body>
<div class="app">
<aside class="sidebar">
 <div class="title">
  <h1>KhajuBridge</h1>
  <span id="status-pill" hx-get="/status-pill" hx-trigger="load, every 10s" class="pill">
   <span class="dot unknown"></span>Loading
  </span>
 </div>

 <div class="card" style="margin-bottom:12px" hx-get="/status-card" hx-trigger="load, every 10s">
  Loading status…
 </div>

 <div id="assurance" hx-get="/assurance" hx-trigger="load, every 15s">
  Loading assurance…
 </div>

 <div style="margin-top:14px">
  <div class="section">Actions</div>
  <div style="display:flex;gap:8px;flex-wrap:wrap">
   <button class="btn" hx-post="/action/apply-firewall" hx-confirm="Re-apply firewall rules now?" hx-target="#action-result">Apply Firewall</button>
   <button class="btn" hx-post="/action/update-cidrs" hx-confirm="Fetch latest Iran CIDRs?" hx-target="#action-result">Update CIDRs</button>
   <button class="btn btn-danger" hx-post="/action/restart-conduit" hx-confirm="Restart conduit.service?" hx-target="#action-result">Restart Conduit</button>
  </div>
  <div id="action-result" style="margin-top:8px;font-size:12px;color:var(--muted)"></div>
 </div>

 <div class="sidebarFooter">
  <div class="sfTitle">KhajuBridge · Open source</div>
  <a class="sfLink" href="https://github.com/delejos/conduit-iran-khajubridge" target="_blank" rel="noopener">github.com/delejos/conduit-iran-khajubridge</a>
 </div>
</aside>

<main class="main">
 <div class="title">
  <div>
   <div class="k">System Overview</div>
   <div class="tagline">KhajuBridge · Iran-optimized Psiphon Conduit firewall</div>
  </div>
 </div>

 <div id="overview" hx-get="/overview" hx-trigger="load, every 8s">
  Loading overview…
 </div>

 <div id="peers" hx-get="/peers" hx-trigger="load, every 30s">
  Loading peer stats…
 </div>

 <div class="logs">
  <div class="logsHead"><b>System Logs</b></div>
  <pre id="logbox" hx-get="/logs" hx-trigger="load" style="max-height:240px;overflow:auto">Loading logs…</pre>
 </div>
</main>
</div>
</body>
</html>
`))

// ── Handlers ──────────────────────────────────────────────────────────────────

func statusPillHandler(w http.ResponseWriter, r *http.Request) {
	_, code := run("systemctl", "is-active", conduitUnit)
	if code == 0 {
		fmt.Fprint(w, `<span class="pill"><span class="dot good"></span>Running</span>`)
	} else {
		fmt.Fprint(w, `<span class="pill"><span class="dot bad"></span>Stopped</span>`)
	}
}

func statusCardHandler(w http.ResponseWriter, r *http.Request) {
	_, code := run("systemctl", "is-active", conduitUnit)
	state := "inactive"
	dot := "bad"
	if code == 0 {
		state = "active"
		dot = "good"
	}
	fmt.Fprintf(w, `<div class="k">Conduit service</div><div class="v" style="font-size:15px"><span class="dot %s" style="display:inline-block;margin-right:6px"></span>%s</div>`,
		dot, template.HTMLEscapeString(state))
}

func assuranceHandler(w http.ResponseWriter, r *http.Request) {
	a := getAssurance()
	dotClass := statusDotClass(a.EnforcementStatus)

	cidrInfo := template.HTMLEscapeString(a.CIDRSource)
	if a.V4Count > 0 {
		cidrInfo += fmt.Sprintf(" (%d v4", a.V4Count)
		if a.V6Count > 0 {
			cidrInfo += fmt.Sprintf(" / %d v6", a.V6Count)
		}
		cidrInfo += ")"
	}

	fmt.Fprintf(w, `
<div class="section">Assurance</div>
<div class="help">Live enforcement state — updated from nftables + state file.</div>
<div class="card">
 <div class="assurance">
  <div class="kv">
   <div class="key">Enforcement</div>
   <div class="val"><span class="dot %s"></span><span class="status">%s</span></div>
  </div>
  <div class="kv">
   <div class="key">Last firewall apply</div>
   <div class="val">%s</div>
  </div>
  <div class="kv">
   <div class="key">Ruleset hash</div>
   <div class="val" style="font-family:monospace">%s</div>
  </div>
  <div class="kv">
   <div class="key">CIDR source</div>
   <div class="val">%s</div>
  </div>
 </div>
</div>`,
		dotClass,
		template.HTMLEscapeString(a.EnforcementStatus),
		template.HTMLEscapeString(a.LastAppliedUTC),
		template.HTMLEscapeString(a.RulesetHash),
		cidrInfo,
	)
}

func overviewHandler(w http.ResponseWriter, r *http.Request) {
	uptime := readUptime()
	cpu := readCPULoadPct()
	used, total := readMemMB()
	cs := readConduitStats()

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
</div>`,
		uptime, cpu, used, total,
		cs.Connected, cs.Connecting,
		cs.UpBps, cs.DownBps,
		cs.UpTotal, cs.DownTotal,
	)
}

func peersHandler(w http.ResponseWriter, r *http.Request) {
	ps := getPeerStats()
	if !ps.Available {
		fmt.Fprint(w, `<div class="section">Peer Countries</div><div class="help" style="margin-top:0">Traffic stats file not found — set TRAFFIC_STATS env var if Conduit data is in a non-default location.</div>`)
		return
	}

	renderTable := func(title string, entries []CountryEntry, iranPct float64) string {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf(`<div class="section">%s</div>`, title))
		sb.WriteString(fmt.Sprintf(`<div class="help">Iran share: <strong class="iran">%.1f%%</strong></div>`, iranPct))
		sb.WriteString(`<div class="card"><table class="tbl"><thead><tr><th>Country</th><th>Total</th><th>Share</th></tr></thead><tbody>`)
		for _, e := range entries {
			cls := ""
			if strings.EqualFold(e.Country, "IR") {
				cls = ` class="iran"`
			}
			// bar width based on iranPct context — just use relative bytes
			sb.WriteString(fmt.Sprintf(`<tr><td%s>%s</td><td>%s</td><td><div class="bar-wrap"><div class="bar" style="width:0"></div></div></td></tr>`,
				cls,
				template.HTMLEscapeString(e.Country),
				template.HTMLEscapeString(e.Total),
			))
		}
		sb.WriteString(`</tbody></table></div>`)
		return sb.String()
	}

	fmt.Fprint(w, renderTable("Inbound Peers (Top 10)", ps.Inbound, ps.IranInPct))
	fmt.Fprint(w, renderTable("Outbound Peers (Top 10)", ps.Outbound, ps.IranOutPct))
}

func logsHandler(w http.ResponseWriter, r *http.Request) {
	out, _ := run("sudo", "-n", "/bin/journalctl", "-u", conduitUnit, "-n", journalTail, "--no-pager")
	fmt.Fprint(w, template.HTMLEscapeString(stripAnsi(out)))
}

func actionHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	action := strings.TrimPrefix(r.URL.Path, "/action/")
	var out string
	var code int

	switch action {
	case "apply-firewall":
		out, code = run("sudo", "-n", khajuScripts+"/apply_firewall.sh")
		// Invalidate caches so next poll reflects new state.
		cacheMu.Lock()
		delete(cache, "assurance")
		cacheMu.Unlock()
	case "update-cidrs":
		out, code = run("sudo", "-n", khajuScripts+"/update_region_cidrs.sh")
	case "restart-conduit":
		out, code = run("sudo", "-n", "/bin/systemctl", "restart", conduitUnit)
	default:
		http.Error(w, "Unknown action", http.StatusNotFound)
		return
	}

	color := "var(--good)"
	if code != 0 {
		color = "var(--bad)"
	}
	fmt.Fprintf(w, `<pre style="color:%s;margin:0;white-space:pre-wrap;font-size:11px">%s</pre>`,
		color, template.HTMLEscapeString(stripAnsi(out)))
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_ = page.Execute(w, nil)
	})
	mux.HandleFunc("/status-pill", statusPillHandler)
	mux.HandleFunc("/status-card", statusCardHandler)
	mux.HandleFunc("/assurance", assuranceHandler)
	mux.HandleFunc("/overview", overviewHandler)
	mux.HandleFunc("/peers", peersHandler)
	mux.HandleFunc("/logs", logsHandler)
	mux.HandleFunc("/action/", actionHandler)

	fmt.Println("KhajuBridge console listening on", listenAddr)
	if err := http.ListenAndServe(listenAddr, lanOnly(mux)); err != nil {
		fmt.Fprintln(os.Stderr, "ERROR:", err)
		os.Exit(1)
	}
}
