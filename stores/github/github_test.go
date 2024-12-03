package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

type TestServer struct {
	mu        sync.Mutex
	callCount int
	reqTimes  []time.Time
}

func (ts *TestServer) handle(w http.ResponseWriter, r *http.Request) {
	ts.mu.Lock()
	ts.callCount++
	currentCount := ts.callCount
	ts.reqTimes = append(ts.reqTimes, time.Now())
	ts.mu.Unlock()

	if currentCount > 5 {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusForbidden)
		// one would think 429 is the right code but github returns 403 on rate limit :/
		//w.WriteHeader(http.StatusTooManyRequests)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func TestRateLimitedTransport_BasicRetry(t *testing.T) {
	// Create transport with test settings
	limiter := rate.NewLimiter(rate.Every(100*time.Millisecond), 1)
	transport := &rateLimitedTransport{
		base:     http.DefaultTransport,
		limiter:  limiter,
		maxRetry: 3,
	}

	ts := &TestServer{}
	server := httptest.NewServer(http.HandlerFunc(ts.handle))
	defer server.Close()

	// Test single request with retries
	req, err := http.NewRequestWithContext(
		context.Background(),
		"GET",
		server.URL,
		nil,
	)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestRateLimitedTransport_ConcurrentRequests(t *testing.T) {
	// Create transport with test settings
	limiter := rate.NewLimiter(rate.Every(100*time.Millisecond), 1)
	transport := &rateLimitedTransport{
		base:     http.DefaultTransport,
		limiter:  limiter,
		maxRetry: 3,
	}

	ts := &TestServer{}
	server := httptest.NewServer(http.HandlerFunc(ts.handle))
	defer server.Close()

	// Make concurrent requests
	var wg sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			req, err := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
			if err != nil {
				t.Logf("Error creating request: %v", err)
				return
			}

			resp, err := transport.RoundTrip(req)
			if err != nil {
				if ctx.Err() != nil {
					// Expected timeout for some requests
					return
				}
				t.Logf("Error in RoundTrip: %v", err)
				return
			}
			resp.Body.Close()
		}()
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Test completed successfully
	case <-time.After(10 * time.Second):
		t.Fatal("Test timed out")
	}

	// Verify we got some rate limit responses
	ts.mu.Lock()
	totalCalls := ts.callCount
	ts.mu.Unlock()

	assert.True(t, totalCalls > 5, "Expected more than 5 calls, got %d", totalCalls)
}

func TestRateLimitedTransport_ExponentialBackoff(t *testing.T) {
	// Create transport with test settings
	limiter := rate.NewLimiter(rate.Every(50*time.Millisecond), 1)
	transport := &rateLimitedTransport{
		base:     http.DefaultTransport,
		limiter:  limiter,
		maxRetry: 3,
	}

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount <= 3 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	start := time.Now()

	req, err := http.NewRequestWithContext(
		context.Background(),
		"GET",
		server.URL,
		nil,
	)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	duration := time.Since(start)

	require.NoError(t, err)
	defer resp.Body.Close()

	// Should have retried 3 times before success
	assert.Equal(t, 4, callCount)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// With exponential backoff, should take at least 3 seconds
	// (1 + 2 + 4 = 7 seconds of backoff minimum)
	assert.True(t, duration >= 3*time.Second,
		"Expected duration >= 3s, got %v", duration)
}

func TestRateLimitedTransport_SecondaryRateLimit(t *testing.T) {
	limiter := rate.NewLimiter(rate.Every(50*time.Millisecond), 1)
	transport := &rateLimitedTransport{
		base:     http.DefaultTransport,
		limiter:  limiter,
		maxRetry: 3,
	}

	// GitHub sometimes sends secondary rate limits with 403 status code
	// and different headers
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount <= 2 {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", time.Now().Add(2*time.Second).Unix()))
			w.WriteHeader(http.StatusForbidden) // GitHub uses 403 for secondary rate limits
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	req, err := http.NewRequestWithContext(
		context.Background(),
		"GET",
		server.URL,
		nil,
	)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 3, callCount)
}

func TestRateLimitedTransport_ContextCancellation(t *testing.T) {
	limiter := rate.NewLimiter(rate.Every(50*time.Millisecond), 1)
	transport := &rateLimitedTransport{
		base:     http.DefaultTransport,
		limiter:  limiter,
		maxRetry: 5,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "2")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	require.NoError(t, err)

	_, err = transport.RoundTrip(req)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestRateLimitedTransport_RetryAfterWithFuture(t *testing.T) {
	limiter := rate.NewLimiter(rate.Every(50*time.Millisecond), 1)
	transport := &rateLimitedTransport{
		base:     http.DefaultTransport,
		limiter:  limiter,
		maxRetry: 3,
	}

	callCount := 0
	// Set reset time to 2 seconds in the future
	resetTime := time.Now().Add(2 * time.Second).Unix()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", resetTime))
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, "GET", server.URL, nil)
	require.NoError(t, err)

	start := time.Now()
	resp, err := transport.RoundTrip(req)
	duration := time.Since(start)

	require.NoError(t, err)
	defer resp.Body.Close()

	// Verify response
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 2, callCount, "Expected exactly 2 calls")

	// Verify timing - should wait close to 2 seconds
	// Allow for some scheduling variance (1.8s minimum)
	assert.True(t, duration >= 1800*time.Millisecond,
		"Expected duration >= 1.8s for rate limit reset, got %v", duration)
	// Shouldn't wait too much longer than necessary
	assert.True(t, duration <= 3*time.Second,
		"Expected duration <= 3s, got %v", duration)
}

func TestRateLimitedTransport_BurstRequests(t *testing.T) {
	limiter := rate.NewLimiter(rate.Every(100*time.Millisecond), 1)
	transport := &rateLimitedTransport{
		base:     http.DefaultTransport,
		limiter:  limiter,
		maxRetry: 3,
	}

	ts := &TestServer{}
	server := httptest.NewServer(http.HandlerFunc(ts.handle))
	defer server.Close()

	// Send burst of requests simultaneously
	const numRequests = 20
	responses := make(chan *http.Response, numRequests)
	errors := make(chan error, numRequests)

	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			req, err := http.NewRequest("GET", server.URL, nil)
			if err != nil {
				errors <- err
				return
			}

			resp, err := transport.RoundTrip(req)
			if err != nil {
				errors <- err
				return
			}
			responses <- resp
		}()
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
		close(responses)
		close(errors)
	}()

	select {
	case <-done:
		duration := time.Since(start)
		// With rate limit of 100ms, 20 requests should take at least 2 seconds
		assert.True(t, duration >= 2*time.Second)
	case <-time.After(10 * time.Second):
		t.Fatal("Test timed out")
	}

	// Check errors
	for err := range errors {
		assert.NoError(t, err)
	}

	// Close all responses
	for resp := range responses {
		resp.Body.Close()
	}
}

func TestRateLimitedTransport_NoRetryAfterHeader(t *testing.T) {
	limiter := rate.NewLimiter(rate.Every(50*time.Millisecond), 1)
	transport := &rateLimitedTransport{
		base:     http.DefaultTransport,
		limiter:  limiter,
		maxRetry: 3,
	}

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount <= 2 {
			// No Retry-After header, should use exponential backoff
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	start := time.Now()
	req, err := http.NewRequestWithContext(
		context.Background(),
		"GET",
		server.URL,
		nil,
	)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	duration := time.Since(start)

	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	// Should have used exponential backoff
	assert.True(t, duration >= 1500*time.Millisecond)
}
