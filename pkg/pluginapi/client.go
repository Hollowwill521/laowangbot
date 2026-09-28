package pluginapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"time"
)

type Client struct {
	URL, Token string
	HTTP       *http.Client
}

func NewHost() (Host, error) {
	raw, token := os.Getenv("LAOWANGBOT_HOST_URL"), os.Getenv("LAOWANGBOT_HOST_TOKEN")
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "http" || net.ParseIP(u.Hostname()) == nil || !net.ParseIP(u.Hostname()).IsLoopback() || token == "" {
		return nil, errors.New("host bridge unavailable; upgrade laowangbot and enable manifest capabilities")
	}
	return Client{URL: raw, Token: token, HTTP: &http.Client{Timeout: 50 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("bridge redirect rejected") }}}, nil
}
func (c Client) Call(ctx context.Context, q Call) (Result, error) {
	var result Result
	b, e := json.Marshal(q)
	if e != nil {
		return result, e
	}
	if len(b) > 1<<20 {
		return result, errors.New("host request too large")
	}
	req, e := http.NewRequestWithContext(ctx, "POST", c.URL+"/call", bytes.NewReader(b))
	if e != nil {
		return result, e
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")
	h := c.HTTP
	if h == nil {
		h = &http.Client{Timeout: 50 * time.Second}
	}
	r, e := h.Do(req)
	if e != nil {
		return result, errors.New("host request failed")
	}
	defer r.Body.Close()
	body, e := io.ReadAll(io.LimitReader(r.Body, (12<<20)+1))
	if e != nil {
		return result, e
	}
	if len(body) > 12<<20 {
		return result, errors.New("host response too large")
	}
	if e = json.Unmarshal(body, &result); e != nil {
		return result, e
	}
	if result.Error != "" {
		return result, errors.New(result.Error)
	}
	if r.StatusCode != 200 {
		return result, fmt.Errorf("host status %d", r.StatusCode)
	}
	return result, nil
}
func StateDir() (string, error) {
	p := os.Getenv("LAOWANGBOT_STATE_DIR")
	if p == "" {
		return "", errors.New("LAOWANGBOT_STATE_DIR is required")
	}
	p, e := filepath.Abs(p)
	if e != nil {
		return "", e
	}
	e = os.MkdirAll(p, 0700)
	return p, e
}
func LoadJSON(path string, out any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, out)
}
func SaveJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(path), ".state-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(append(b, '\n')); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), path)
}
func Serve(handler func(context.Context, Request) Response) error {
	ctx, cancel := signal.NotifyContext(context.Background(), signals()...)
	defer cancel()
	return ServeIO(ctx, os.Stdin, os.Stdout, handler)
}
func ServeIO(ctx context.Context, in io.Reader, out io.Writer, handler func(context.Context, Request) Response) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	enc := json.NewEncoder(out)
	for scanner.Scan() {
		if e := ctx.Err(); e != nil {
			return e
		}
		var q Request
		r := Response{Version: 1}
		if e := json.Unmarshal(scanner.Bytes(), &q); e != nil {
			r.Error = "invalid request"
		} else if q.Version != 1 {
			r.Error = "unsupported protocol"
		} else {
			r = handler(ctx, q)
			r.Version = 1
		}
		if e := enc.Encode(r); e != nil {
			return e
		}
	}
	return scanner.Err()
}
