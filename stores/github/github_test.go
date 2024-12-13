package github

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"
)

// mockTransport implements http.RoundTripper for testing
type mockTransport struct {
	responses []*http.Response
	requests  []*http.Request
	index     int
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	m.requests = append(m.requests, req)
	if m.index >= len(m.responses) {
		return m.responses[len(m.responses)-1], nil
	}
	resp := m.responses[m.index]
	m.index++
	return resp, nil
}

// mockResponseBody implements io.ReadCloser for testing
type mockResponseBody struct {
	io.Reader
	closeCount int
}

func (m *mockResponseBody) Close() error {
	m.closeCount++
	return nil
}

func createResponse(statusCode int, headers map[string]string, body string) *http.Response {
	resp := &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       &mockResponseBody{Reader: strings.NewReader(body)},
	}
	for k, v := range headers {
		resp.Header.Set(k, v)
	}
	return resp
}

func TestRateLimitedTransport_CalculateRetryDelay(t *testing.T) {
	tests := []struct {
		name          string
		response      *http.Response
		expectedDelay time.Duration
	}{
		{
			name: "403 with Retry-After header",
			response: createResponse(403, map[string]string{
				"Retry-After": "30",
			}, ""),
			expectedDelay: 30 * time.Second,
		},
		{
			name:          "403 without Retry-After header",
			response:      createResponse(403, map[string]string{}, ""),
			expectedDelay: 60 * time.Second,
		},
		{
			name: "Rate limit with reset time",
			response: createResponse(429, map[string]string{
				"X-RateLimit-Remaining": "0",
				"X-RateLimit-Reset":     strconv.FormatInt(time.Now().Add(10*time.Second).Unix(), 10),
			}, ""),
			expectedDelay: 12 * time.Second, // 10 seconds + 2 second buffer
		},
		{
			name:          "Default backoff",
			response:      createResponse(429, map[string]string{}, ""),
			expectedDelay: 10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &rateLimitedTransport{
				base:    http.DefaultTransport,
				limiter: rate.NewLimiter(rate.Every(time.Second), 1),
			}

			delay := transport.calculateRetryDelay(tt.response)

			// Allow for small variations in time-based tests
			if tt.name == "Rate limit with reset time" {
				assert.InDelta(t, tt.expectedDelay, delay, float64(3*time.Second))
			} else {
				assert.Equal(t, tt.expectedDelay, delay)
			}
		})
	}
}

func TestRateLimitedTransport_ShouldRetry(t *testing.T) {
	tests := []struct {
		name     string
		response *http.Response
		want     bool
	}{
		{
			name:     "Should retry on 429",
			response: &http.Response{StatusCode: 429},
			want:     true,
		},
		{
			name:     "Should retry on 403",
			response: &http.Response{StatusCode: 403},
			want:     true,
		},
		{
			name:     "Should retry on 500",
			response: &http.Response{StatusCode: 500},
			want:     true,
		},
		{
			name:     "Should not retry on 200",
			response: &http.Response{StatusCode: 200},
			want:     false,
		},
		{
			name:     "Should not retry on 400",
			response: &http.Response{StatusCode: 400},
			want:     false,
		},
	}

	transport := &rateLimitedTransport{
		base:    http.DefaultTransport,
		limiter: rate.NewLimiter(rate.Every(time.Second), 1),
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := transport.shouldRetry(tt.response)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRateLimitedTransport_RoundTrip(t *testing.T) {
	tests := []struct {
		name            string
		responses       []*http.Response
		expectedRetries int
		expectError     bool
		contextTimeout  time.Duration
	}{
		{
			name: "Success on first try",
			responses: []*http.Response{
				createResponse(200, nil, "success"),
			},
			expectedRetries: 0,
			expectError:     false,
		},
		{
			name: "Success after rate limit",
			responses: []*http.Response{
				createResponse(429, map[string]string{
					"Retry-After": "1",
				}, "rate limited"),
				createResponse(200, nil, "success"),
			},
			expectedRetries: 1,
			expectError:     false,
		},
		{
			name: "Context cancellation",
			responses: []*http.Response{
				createResponse(429, map[string]string{
					"Retry-After": "5",
				}, "rate limited"),
			},
			expectedRetries: 0,
			expectError:     true,
			contextTimeout:  100 * time.Millisecond,
		},
		{
			name: "Multiple retries before success",
			responses: []*http.Response{
				createResponse(429, map[string]string{"Retry-After": "1"}, "rate limited"),
				createResponse(403, map[string]string{"Retry-After": "1"}, "forbidden"),
				createResponse(500, nil, "server error"),
				createResponse(200, nil, "success"),
			},
			expectedRetries: 3,
			expectError:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			if tt.contextTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tt.contextTimeout)
				defer cancel()
			}

			mock := &mockTransport{responses: tt.responses}
			transport := &rateLimitedTransport{
				base:    mock,
				limiter: rate.NewLimiter(rate.Every(time.Millisecond), 1), // Fast limiter for tests
			}

			req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/test", nil)
			require.NoError(t, err)

			resp, err := transport.RoundTrip(req)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, resp)
			assert.Equal(t, 200, resp.StatusCode)
			assert.Equal(t, tt.expectedRetries+1, len(mock.requests))

			// Verify all response bodies were closed
			for _, res := range tt.responses[:len(mock.requests)-1] {
				body := res.Body.(*mockResponseBody)
				assert.Equal(t, 1, body.closeCount, "Response body should be closed after retry")
			}
		})
	}
}

func TestRateLimitedTransport_RequestBodyHandling(t *testing.T) {
	responses := []*http.Response{
		createResponse(429, map[string]string{"Retry-After": "1"}, "rate limited"),
		createResponse(200, nil, "success"),
	}

	mock := &mockTransport{responses: responses}
	transport := &rateLimitedTransport{
		base:    mock,
		limiter: rate.NewLimiter(rate.Every(time.Millisecond), 1),
	}

	// Create a request with a body
	body := "test request body"
	req, err := http.NewRequest("POST", "https://api.github.com/test",
		strings.NewReader(body))
	require.NoError(t, err)

	// Ensure the body can be read multiple times
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(body)), nil
	}

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	// Verify the body was sent in both requests
	for _, r := range mock.requests {
		bodyBytes, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, body, string(bodyBytes))
	}
}
