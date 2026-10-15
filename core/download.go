package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Client is the HTTP client used for all downloads.
// One shared transport: keep-alive, HTTP/2, many idle connections per host.
var Client = &http.Client{Timeout: 0, Transport: &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	ForceAttemptHTTP2:     true,
	MaxIdleConns:          64,
	MaxIdleConnsPerHost:   32,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   15 * time.Second,
	ResponseHeaderTimeout: 60 * time.Second,
	ExpectContinueTimeout: time.Second,
}}

// RetryBase is the first retry delay; it doubles on each attempt.
var RetryBase = time.Second

const userAgent = "A2-Client-Launcher"

// FetchSmall downloads a small resource (manifest, signature) into memory.
func FetchSmall(ctx context.Context, url string, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Cache-Control", "no-cache")
	c := *Client
	c.Timeout = 30 * time.Second
	resp, err := c.Do(req)
	if err != nil {
		return nil, friendlyNet(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("the update server answered %s for %s", resp.Status, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, max))
}

func friendlyNet(err error) error {
	if errors.Is(err, context.Canceled) {
		return err
	}
	return fmt.Errorf("could not reach the update server (check your internet connection): %v", err)
}

// DownloadVerified downloads url to dst, resuming from an existing partial
// file, and checks size and SHA-256. On success dst holds the verified file.
// onBytes is called with the number of new bytes written (may be nil).
func DownloadVerified(ctx context.Context, url, dst string, size int64, sha string, onBytes func(int64)) error {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(RetryBase * time.Duration(1<<(attempt-1))):
			}
		}
		lastErr = downloadOnce(ctx, url, dst, size, onBytes)
		if lastErr == nil {
			got, err := HashFile(dst)
			if err != nil {
				return err
			}
			if got == sha {
				return nil
			}
			os.Remove(dst) // corrupt: start over
			lastErr = fmt.Errorf("downloaded file failed its checksum")
			continue
		}
		if errors.Is(lastErr, context.Canceled) {
			return lastErr
		}
	}
	return lastErr
}

func downloadOnce(ctx context.Context, url, dst string, size int64, onBytes func(int64)) error {
	f, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	have, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	if have > size {
		if err := f.Truncate(0); err != nil {
			return err
		}
		have, _ = f.Seek(0, io.SeekStart)
	}
	if have == size {
		return nil // already complete; caller verifies
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	resp, err := Client.Do(req)
	if err != nil {
		return friendlyNet(err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusPartialContent:
		if onBytes != nil && have > 0 {
			onBytes(have)
		}
	case http.StatusOK: // server ignored Range: restart
		if err := f.Truncate(0); err != nil {
			return err
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
	case http.StatusRequestedRangeNotSatisfiable:
		f.Truncate(0)
		return fmt.Errorf("download restart needed")
	default:
		return fmt.Errorf("the update server answered %s", resp.Status)
	}
	buf := make([]byte, 256*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return fmt.Errorf("could not write to disk (is it full?): %v", err)
			}
			if onBytes != nil {
				onBytes(int64(n))
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return friendlyNet(rerr)
		}
	}
}

// sha256Hex is a helper for tests and self-update.
func sha256Hex(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
