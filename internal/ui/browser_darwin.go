//go:build darwin

package ui

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// navigationScript reuses an existing vaulty-keeper UI tab. %s is the browser
// application name, interpolated as a literal so the compiler binds that app's
// dictionary (a variable app name makes `tabs of w` fail with error -1700).
// The script looks for a tab whose URL is the UI itself (loopback host with a
// ?t= access-token query), navigates that tab to the new URL and brings the
// window to the front. It prints "yes" only when such a tab was reused.
//
// Property differences between browsers are absorbed by `try` blocks: an app
// that does not understand `active tab index` / `index of w` simply leaves the
// tab state as it was, which is fine because the URL is what matters.
const navigationScript = `on run argv
  set newURL to item 1 of argv
  if not (application "%s" is running) then return "no"
  tell application "%s"
    repeat with w in windows
      set i to 1
      repeat with t in tabs of w
        if URL of t contains "127.0.0.1" and URL of t contains "?t=" then
          set URL of t to newURL
          try
            set active tab index of w to i
          end try
          try
            set index of w to 1
          end try
          return "yes"
        end if
        set i to i + 1
      end repeat
    end repeat
  end tell
  return "no"
end run`

// navigableBrowsers are tried in order; the first one holding a UI tab wins.
var navigableBrowsers = []string{
	"Google Chrome",
	"Arc",
	"Microsoft Edge",
	"Brave Browser",
	"Chromium",
	"Opera",
	"Safari",
}

// openURL reuses an already-open vaulty-keeper UI tab when possible, so
// repeated `vaulty-keeper ui` runs do not pile up new windows/tabs; it only
// falls back to opening a brand-new tab when no existing tab can be reused.
func openURL(url string) error {
	if reuseUITab(url) {
		return nil
	}
	return exec.Command("open", url).Start()
}

// reuseUITab asks each scriptable browser to navigate its matching tab. It
// returns true as soon as one browser confirms a tab was reused.
func reuseUITab(url string) bool {
	for _, app := range navigableBrowsers {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		out, err := exec.CommandContext(ctx, "osascript", "-e", scriptFor(app), url).Output()
		cancel()
		if err == nil && strings.TrimSpace(string(out)) == "yes" {
			return true
		}
	}
	return false
}

// scriptFor embeds the browser name into the navigation script. App names are
// plain words ("Google Chrome", "Brave Browser"), so no quoting is needed.
func scriptFor(app string) string {
	return strings.ReplaceAll(navigationScript, "%s", app)
}
