package routes

import (
	"net/http"
	"testing"
	"time"
)

func TestMaskEmail(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		expected string
	}{
		{
			name:     "normal email",
			email:    "user@example.com",
			expected: "u***@example.com",
		},
		{
			name:     "single char local part",
			email:    "u@example.com",
			expected: "u***@example.com",
		},
		{
			name:     "empty email",
			email:    "",
			expected: "[empty]",
		},
		{
			name:     "no @ sign",
			email:    "invalidemail",
			expected: "[invalid]",
		},
		{
			name:     "long local part",
			email:    "verylongemail@domain.org",
			expected: "v***@domain.org",
		},
		{
			name:     "subdomain",
			email:    "admin@mail.example.com",
			expected: "a***@mail.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := maskEmail(tt.email)
			if result != tt.expected {
				t.Errorf("maskEmail(%q) = %q, want %q", tt.email, result, tt.expected)
			}
		})
	}
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		remoteAddr string
		expected   string
	}{
		{
			name:       "X-Forwarded-For single IP",
			headers:    map[string]string{"X-Forwarded-For": "192.168.1.100"},
			remoteAddr: "127.0.0.1:8080",
			expected:   "192.168.1.100",
		},
		{
			name:       "X-Forwarded-For multiple IPs",
			headers:    map[string]string{"X-Forwarded-For": "10.0.0.1, 192.168.1.1, 172.16.0.1"},
			remoteAddr: "127.0.0.1:8080",
			expected:   "10.0.0.1",
		},
		{
			name:       "X-Real-IP",
			headers:    map[string]string{"X-Real-IP": "10.20.30.40"},
			remoteAddr: "127.0.0.1:8080",
			expected:   "10.20.30.40",
		},
		{
			name:       "X-Forwarded-For takes precedence over X-Real-IP",
			headers:    map[string]string{"X-Forwarded-For": "1.2.3.4", "X-Real-IP": "5.6.7.8"},
			remoteAddr: "127.0.0.1:8080",
			expected:   "1.2.3.4",
		},
		{
			name:       "fallback to RemoteAddr",
			headers:    map[string]string{},
			remoteAddr: "192.168.0.1:54321",
			expected:   "192.168.0.1",
		},
		{
			name:       "RemoteAddr without port",
			headers:    map[string]string{},
			remoteAddr: "10.0.0.50",
			expected:   "10.0.0.50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest("POST", "/api/custom/auth/elevate", nil)
			req.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}

			result := getClientIP(req)
			if result != tt.expected {
				t.Errorf("getClientIP() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestRateLimiter_Allow(t *testing.T) {
	// Create a rate limiter with 3 requests per 100ms window
	rl := newRateLimiter(3, 100*time.Millisecond)

	ip := "192.168.1.1"

	// First 3 requests should be allowed
	for i := 0; i < 3; i++ {
		if !rl.allow(ip) {
			t.Errorf("Request %d should be allowed", i+1)
		}
	}

	// 4th request should be denied
	if rl.allow(ip) {
		t.Error("4th request should be denied (rate limit exceeded)")
	}

	// Different IP should still be allowed
	if !rl.allow("192.168.1.2") {
		t.Error("Different IP should be allowed")
	}

	// Wait for window to expire
	time.Sleep(150 * time.Millisecond)

	// Should be allowed again after window expires
	if !rl.allow(ip) {
		t.Error("Request should be allowed after window expires")
	}
}

func TestRateLimiter_Cleanup(t *testing.T) {
	rl := newRateLimiter(5, 50*time.Millisecond)

	// Add some requests
	rl.allow("10.0.0.1")
	rl.allow("10.0.0.2")

	// Verify entries exist
	rl.mu.RLock()
	initialCount := len(rl.requests)
	rl.mu.RUnlock()

	if initialCount != 2 {
		t.Errorf("Expected 2 entries, got %d", initialCount)
	}

	// Wait for window + cleanup cycle
	time.Sleep(100 * time.Millisecond)

	// Trigger cleanup manually
	rl.cleanup()

	// Verify entries were cleaned up
	rl.mu.RLock()
	afterCount := len(rl.requests)
	rl.mu.RUnlock()

	if afterCount != 0 {
		t.Errorf("Expected 0 entries after cleanup, got %d", afterCount)
	}
}

func TestRateLimiter_ConcurrentAccess(t *testing.T) {
	rl := newRateLimiter(100, time.Second)

	done := make(chan bool)

	// Spawn multiple goroutines accessing the rate limiter concurrently
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 20; j++ {
				rl.allow("concurrent-test-ip")
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// If we got here without panicking, the concurrent access is safe
}
