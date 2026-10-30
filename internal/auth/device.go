package auth

import "strings"

// deviceLabel names the browser and system a sign-in came from, as a person
// would ("Firefox on macOS"), so the account's device list is recognisable.
// Only this label is kept; the User-Agent itself is not stored. An agent it
// does not recognise gives "".
func deviceLabel(userAgent string) string {
	ua := strings.ToLower(userAgent)
	browser := firstMatch(ua, []match{
		{"edg/", "Edge"}, {"edga/", "Edge"}, {"edgios/", "Edge"},
		{"opr/", "Opera"}, {"samsungbrowser/", "Samsung Internet"},
		{"firefox/", "Firefox"}, {"fxios/", "Firefox"},
		{"crios/", "Chrome"}, {"chrome/", "Chrome"},
		{"safari/", "Safari"},
	})
	system := firstMatch(ua, []match{
		{"iphone", "iPhone"}, {"ipad", "iPad"},
		{"android", "Android"},
		{"windows", "Windows"},
		{"cros", "ChromeOS"},
		{"mac os x", "macOS"}, {"macintosh", "macOS"},
		{"linux", "Linux"},
	})
	switch {
	case browser != "" && system != "":
		return browser + " on " + system
	case browser != "":
		return browser
	default:
		return system
	}
}

type match struct{ token, name string }

// firstMatch returns the name of the first token found in s; order matters,
// since most agents also claim to be the browsers they descend from.
func firstMatch(s string, ms []match) string {
	for _, m := range ms {
		if strings.Contains(s, m.token) {
			return m.name
		}
	}
	return ""
}
