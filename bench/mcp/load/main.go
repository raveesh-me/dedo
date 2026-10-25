// Command load drives one MCP tool call against a server's /mcp endpoint
// from concurrent clients for a fixed time, and prints the latency
// distribution and throughput as one JSON line. Every call must succeed:
// a transport error, a JSON-RPC error or a tool error stops the run, so a
// result only ever counts calls that passed the server's security checks.
//
//	load -url http://todo:8080/mcp -token writer -tool get_todo -args '{"name":"todos/bench"}'
//
// With -session, each client first opens an MCP session (initialize, then
// notifications/initialized) and sends its Mcp-Session-Id on every call, as
// stateful servers require. Without it, calls go straight to tools/call, as
// stateless servers allow.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

// protocolVersion is the MCP revision the clients speak.
const protocolVersion = "2025-06-18"

type config struct {
	url, token, tool, args, label string
	session, once                 bool
	clients                       int
	warmup, duration              time.Duration
}

func main() {
	var cfg config
	flag.StringVar(&cfg.url, "url", "", "the server's MCP endpoint")
	flag.StringVar(&cfg.token, "token", "", "bearer token sent on every request")
	flag.StringVar(&cfg.tool, "tool", "", "tool to call")
	flag.StringVar(&cfg.args, "args", "{}", "tool arguments, as JSON")
	flag.StringVar(&cfg.label, "label", "", "name of the run, copied to the output")
	flag.BoolVar(&cfg.session, "session", false, "open an MCP session per client first")
	flag.BoolVar(&cfg.once, "once", false, "make one call, such as to create test data, and print nothing")
	flag.IntVar(&cfg.clients, "c", 1, "concurrent clients, each calling in a loop")
	flag.DurationVar(&cfg.warmup, "warmup", 5*time.Second, "time to call before measuring")
	flag.DurationVar(&cfg.duration, "d", 20*time.Second, "time to measure")
	flag.Parse()
	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "load:", err)
		os.Exit(1)
	}
}

// result is one run, as printed.
type result struct {
	Label   string  `json:"label"`
	Tool    string  `json:"tool"`
	Clients int     `json:"clients"`
	Calls   int     `json:"calls"`
	RPS     float64 `json:"rps"`
	P50     float64 `json:"p50_ms"`
	P90     float64 `json:"p90_ms"`
	P99     float64 `json:"p99_ms"`
	Max     float64 `json:"max_ms"`
}

func run(cfg config) error {
	transport := &http.Transport{MaxIdleConnsPerHost: cfg.clients, MaxConnsPerHost: cfg.clients}
	httpClient := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	callers := make([]*caller, cfg.clients)
	for i := range callers {
		c := &caller{cfg: cfg, http: httpClient}
		if cfg.session {
			if err := c.open(); err != nil {
				return fmt.Errorf("client %d: opening a session: %w", i, err)
			}
		}
		callers[i] = c
	}
	if cfg.once {
		return callers[0].call()
	}

	var (
		mu        sync.Mutex
		latencies []time.Duration
		failure   error
		wg        sync.WaitGroup
	)
	start := time.Now()
	measureFrom, stop := start.Add(cfg.warmup), start.Add(cfg.warmup+cfg.duration)
	for _, c := range callers {
		wg.Go(func() {
			var own []time.Duration
			for {
				t0 := time.Now()
				if t0.After(stop) {
					break
				}
				if err := c.call(); err != nil {
					mu.Lock()
					failure = errors.Join(failure, err)
					mu.Unlock()
					return
				}
				if t0.After(measureFrom) {
					own = append(own, time.Since(t0))
				}
			}
			mu.Lock()
			latencies = append(latencies, own...)
			mu.Unlock()
		})
	}
	wg.Wait()
	if failure != nil {
		return failure
	}
	if len(latencies) == 0 {
		return errors.New("no calls completed in the measured window")
	}
	slices.Sort(latencies)
	ms := func(q float64) float64 {
		return float64(latencies[int(q*float64(len(latencies)-1))].Microseconds()) / 1000
	}
	return json.NewEncoder(os.Stdout).Encode(result{
		Label: cfg.label, Tool: cfg.tool, Clients: cfg.clients, Calls: len(latencies),
		RPS: float64(len(latencies)) / cfg.duration.Seconds(),
		P50: ms(0.50), P90: ms(0.90), P99: ms(0.99), Max: ms(1),
	})
}

// caller is one client: its session, if any, and its request body.
type caller struct {
	cfg     config
	http    *http.Client
	session string
}

// open starts an MCP session, as a stateful server requires before any call.
func (c *caller) open() error {
	init := `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"` + protocolVersion +
		`","capabilities":{},"clientInfo":{"name":"load","version":"v1"}}}`
	res, _, err := c.post(init)
	if err != nil {
		return err
	}
	c.session = res.Header.Get("Mcp-Session-Id")
	if c.session == "" {
		return errors.New("the server returned no Mcp-Session-Id")
	}
	_, _, err = c.post(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	return err
}

// call makes one tool call and fails unless it succeeds.
func (c *caller) call() error {
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + c.cfg.tool + `","arguments":` + c.cfg.args + `}}`
	_, msg, err := c.post(body)
	if err != nil {
		return err
	}
	var out struct {
		Error  *json.RawMessage `json:"error"`
		Result *struct {
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal(msg, &out); err != nil {
		return fmt.Errorf("%s: %w: %s", c.cfg.tool, err, msg)
	}
	if out.Error != nil || out.Result == nil || out.Result.IsError {
		return fmt.Errorf("%s failed: %s", c.cfg.tool, msg)
	}
	return nil
}

// post sends one JSON-RPC message and returns the response and its JSON-RPC
// message, read from a JSON body or from the first event of an SSE stream.
func (c *caller) post(body string) (*http.Response, []byte, error) {
	req, err := http.NewRequest(http.MethodPost, c.cfg.url, strings.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+c.cfg.token)
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
		req.Header.Set("Mcp-Protocol-Version", protocolVersion)
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusAccepted {
		_, err := io.Copy(io.Discard, res.Body)
		return res, nil, err
	}
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		return nil, nil, fmt.Errorf("status %d: %s", res.StatusCode, b)
	}
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		b, err := io.ReadAll(res.Body)
		return res, b, err
	}
	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(nil, 16<<20)
	for scanner.Scan() {
		if data, ok := bytes.CutPrefix(scanner.Bytes(), []byte("data:")); ok {
			msg := bytes.TrimSpace(data)
			_, err := io.Copy(io.Discard, res.Body)
			return res, slices.Clone(msg), err
		}
	}
	return nil, nil, fmt.Errorf("event stream without data: %v", scanner.Err())
}
