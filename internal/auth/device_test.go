package auth

import "testing"

func TestDeviceLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, ua, want string
	}{
		{name: "Firefox on macOS", ua: "Mozilla/5.0 (Macintosh; Intel Mac OS X 14.6; rv:131.0) Gecko/20100101 Firefox/131.0", want: "Firefox on macOS"},
		{name: "Chrome on Windows", ua: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36", want: "Chrome on Windows"},
		{name: "Edge claims Chrome and Safari", ua: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36 Edg/130.0.0.0", want: "Edge on Windows"},
		{name: "Safari on iPhone", ua: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1", want: "Safari on iPhone"},
		{name: "Chrome on iPhone", ua: "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/130.0 Mobile/15E148 Safari/604.1", want: "Chrome on iPhone"},
		{name: "Chrome on Android", ua: "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Mobile Safari/537.36", want: "Chrome on Android"},
		{name: "Samsung Internet", ua: "Mozilla/5.0 (Linux; Android 14; SM-S918B) AppleWebKit/537.36 (KHTML, like Gecko) SamsungBrowser/26.0 Chrome/122.0.0.0 Mobile Safari/537.36", want: "Samsung Internet on Android"},
		{name: "Firefox on Linux", ua: "Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0", want: "Firefox on Linux"},
		{name: "a script", ua: "curl/8.7.1", want: ""},
		{name: "nothing", ua: "", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := deviceLabel(tc.ua); got != tc.want {
				t.Fatalf("deviceLabel = %q, want %q", got, tc.want)
			}
		})
	}
}
