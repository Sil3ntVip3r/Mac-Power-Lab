package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Sil3ntVip3r/Mac-Power-Lab/internal/config"
)

func TestLoopbackTokenAPI(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	tokenFile := filepath.Join(cfg.DataDir, "token")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, "127.0.0.1:0", tokenFile, NewEngine(cfg), false) }()
	addressFile := tokenFile + ".address"
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(addressFile); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	addressBytes, err := os.ReadFile(addressFile)
	if err != nil {
		t.Fatal(err)
	}
	tokenBytes, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + strings.TrimSpace(string(addressBytes))
	resp, err := http.Get(base + "/health")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health=%d", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodGet, base+"/status", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized=%d", resp.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodGet, base+"/status", nil)
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(tokenBytes)))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("authorized=%d", resp.StatusCode)
	}

	authHeader := "Bearer " + strings.TrimSpace(string(tokenBytes))

	req, _ = http.NewRequest(http.MethodGet, base+"/benchmarks", nil)
	req.Header.Set("Authorization", authHeader)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var catalog []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || len(catalog) < 10 {
		t.Fatalf("benchmark catalog status=%d entries=%d", resp.StatusCode, len(catalog))
	}

	payload := []byte(`{"name":"custom","custom":{"display_name":"Test","required_power_source":"any","cpu":false,"gpu":false,"memory":false,"gpu_profile":"high","memory_mb":0,"workload_seconds":60,"baseline_seconds":0,"cooldown_seconds":0}}`)
	req, _ = http.NewRequest(http.MethodPost, base+"/benchmark/start", bytes.NewReader(payload))
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid custom status=%d", resp.StatusCode)
	}

	// Exercise handlers concurrently. The v1.2 implementation shared the
	// Serve-local err variable across handlers and raced here under -race.
	var wait sync.WaitGroup
	requestErrors := make(chan error, 80)
	for i := 0; i < 40; i++ {
		wait.Add(2)
		go func() {
			defer wait.Done()
			req, _ := http.NewRequest(http.MethodGet, base+"/status", nil)
			req.Header.Set("Authorization", authHeader)
			response, requestErr := http.DefaultClient.Do(req)
			if requestErr != nil {
				requestErrors <- requestErr
				return
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				requestErrors <- fmt.Errorf("status endpoint returned %d", response.StatusCode)
			}
		}()
		go func() {
			defer wait.Done()
			body := bytes.NewBufferString(`{"name":"custom","unexpected":true}`)
			req, _ := http.NewRequest(http.MethodPost, base+"/benchmark/start", body)
			req.Header.Set("Authorization", authHeader)
			req.Header.Set("Content-Type", "application/json")
			response, requestErr := http.DefaultClient.Do(req)
			if requestErr != nil {
				requestErrors <- requestErr
				return
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusBadRequest {
				requestErrors <- fmt.Errorf("invalid benchmark returned %d", response.StatusCode)
			}
		}()
	}
	wait.Wait()
	close(requestErrors)
	for requestErr := range requestErrors {
		t.Error(requestErr)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not stop")
	}
}

func TestRequireLoopbackListener(t *testing.T) {
	if err := requireLoopbackListener(&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1}); err != nil {
		t.Fatal(err)
	}
	if err := requireLoopbackListener(&net.TCPAddr{IP: net.ParseIP("0.0.0.0"), Port: 1}); err == nil {
		t.Fatal("expected wildcard address rejection")
	}
}
