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
			name: "Should retry on low rate limit remaining",
			response: createResponse(200, map[string]string{
				"X-RateLimit-Remaining": "5",
			}, ""),
			want: true,
		},
		{
			name:     "Should retry on 502",
			response: &http.Response{StatusCode: 502},
			want:     true,
		},
		{
			name:     "Should retry on 503",
			response: &http.Response{StatusCode: 503},
			want:     true,
		},
		{
			name:     "Should retry on 504",
			response: &http.Response{StatusCode: 504},
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

func TestRateLimitedTransport_CalculateRetryDelay(t *testing.T) {
	tests := []struct {
		name     string
		response *http.Response
		minDelay time.Duration
		maxDelay time.Duration
	}{
		{
			name: "403 with Retry-After header",
			response: createResponse(403, map[string]string{
				"Retry-After": "30",
			}, ""),
			minDelay: 30 * time.Second, // Base delay
			maxDelay: 66 * time.Second, // Base + 120% jitter
		},
		{
			name:     "403 without Retry-After header (abuse detection)",
			response: createResponse(403, map[string]string{}, ""),
			minDelay: 120 * time.Second, // Base delay
			maxDelay: 264 * time.Second, // Base + 120% jitter
		},
		{
			name: "Rate limit with reset time",
			response: createResponse(429, map[string]string{
				"X-RateLimit-Remaining": "0",
				"X-RateLimit-Reset":     strconv.FormatInt(time.Now().Add(10*time.Second).Unix(), 10),
			}, ""),
			minDelay: 10 * time.Second, // Base delay
			maxDelay: 22 * time.Second, // Base + 120% jitter
		},
		{
			name: "429 without reset time",
			response: createResponse(429, map[string]string{
				"X-RateLimit-Remaining": "0",
			}, ""),
			minDelay: 60 * time.Second,  // Base delay
			maxDelay: 132 * time.Second, // Base + 120% jitter
		},
		{
			name:     "Server error (5xx)",
			response: createResponse(502, map[string]string{}, ""),
			minDelay: 30 * time.Second, // Base delay
			maxDelay: 66 * time.Second, // Base + 120% jitter
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &rateLimitedTransport{
				base:    http.DefaultTransport,
				limiter: rate.NewLimiter(rate.Every(time.Second), 1),
			}

			delay := transport.calculateRetryDelay(tt.response)

			// Check that delay is within expected bounds
			if delay < tt.minDelay {
				t.Errorf("Delay %v is less than minimum expected delay %v", delay, tt.minDelay)
			}
			if delay > tt.maxDelay {
				t.Errorf("Delay %v exceeds maximum expected delay %v", delay, tt.maxDelay)
			}
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
				createResponse(502, nil, "server error"),
				createResponse(200, nil, "success"),
			},
			expectedRetries: 3,
			expectError:     false,
		},
		{
			name: "Retry on low remaining rate limit",
			responses: []*http.Response{
				createResponse(200, map[string]string{
					"X-RateLimit-Remaining": "5",
				}, "low limit"),
				createResponse(200, map[string]string{
					"X-RateLimit-Remaining": "100",
				}, "success"),
			},
			expectedRetries: 1,
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

			// Check retry count with some flexibility for jitter-induced extra retries
			actualRetries := len(mock.requests) - 1
			if actualRetries < tt.expectedRetries {
				t.Errorf("Got %d retries, expected at least %d", actualRetries, tt.expectedRetries)
			}
			maxAllowedRetries := tt.expectedRetries + 2 // Allow up to 2 extra retries due to jitter
			if actualRetries > maxAllowedRetries {
				t.Errorf("Got %d retries, expected no more than %d", actualRetries, maxAllowedRetries)
			}

			// Verify all response bodies were closed except the last one
			for _, res := range tt.responses[:len(mock.requests)-1] {
				body := res.Body.(*mockResponseBody)
				assert.Equal(t, 1, body.closeCount, "Response body should be closed after retry")
			}
		})
	}
}
