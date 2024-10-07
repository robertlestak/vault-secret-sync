package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/go-github/v62/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

func TestRateLimitedTransport(t *testing.T) {
	limiter := rate.NewLimiter(rate.Every(100*time.Millisecond), 1) // 1 request per 100ms
	transport := &rateLimitedTransport{
		base:     http.DefaultTransport,
		limiter:  limiter,
		maxRetry: 3,
	}

	// Create a test server that returns 429 (Too Many Requests) for the first two calls
	// and 200 (OK) for the third call
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Header().Set("Retry-After", "1")
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Success"))
		}
	}))
	defer server.Close()

	// Create a new request
	req, err := http.NewRequest("GET", server.URL, nil)
	require.NoError(t, err)

	// Use the rate limited transport to make the request
	start := time.Now()
	resp, err := transport.RoundTrip(req)
	duration := time.Since(start)

	// Check the results
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, 3, callCount)
	assert.True(t, duration >= 200*time.Millisecond, "Expected duration to be at least 200ms due to rate limiting and retries")
}

func TestGitHubClientWithRateLimiting(t *testing.T) {
	// Create a test server that simulates GitHub API
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount > 5 { // Simulate rate limit after 5 calls
			w.WriteHeader(http.StatusTooManyRequests)
			w.Header().Set("Retry-After", "1")
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"login":"testuser"}`))
		}
	}))
	defer server.Close()

	// Create a client that uses our test server
	client := github.NewClient(nil)
	u := server.URL + "/"
	pu, err := url.Parse(u)
	require.NoError(t, err)
	client.BaseURL = pu

	// Use the client to make multiple requests
	for i := 0; i < 10; i++ {
		user, _, err := client.Users.Get(context.Background(), "testuser")
		if i < 5 {
			assert.NoError(t, err)
			assert.Equal(t, "testuser", user.GetLogin())
		} else {
			assert.Error(t, err)
			assert.Nil(t, user)
		}
	}

	// Check that we made the expected number of calls
	assert.Equal(t, 10, callCount)
}
