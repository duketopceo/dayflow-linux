package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// appnames.go resolves Hyprland window classes and model-reported app names
// ("com.mitchellh.ghostty", "chrome-x.com__-Default", "agent zero") to proper
// display names ("Ghostty", "X", "Agent Zero") using freedesktop .desktop
// entries, with a cleaned-up fallback when no entry matches.

var (
	appNameOnce sync.Once
	appNameByID map[string]string // lowercase desktop-id / StartupWMClass / tail -> Name
)

func appDisplayName(cls string) string {
	if cls == "" {
		return ""
	}
	appNameOnce.Do(buildAppNameMap)
	lc := strings.ToLower(strings.TrimSpace(cls))
	if n, ok := appNameByID[lc]; ok {
		return n
	}
	tail := lc
	if i := strings.LastIndex(tail, "."); i >= 0 {
		tail = tail[i+1:]
	}
	if n, ok := appNameByID[tail]; ok {
		return n
	}
	return titleize(cls)
}

func catDisplay(cat string) string {
	if cat == "" {
		return ""
	}
	return strings.ToUpper(cat[:1]) + cat[1:]
}

// titleize turns "com.mitchellh.ghostty" into "Ghostty", "agent zero" into
// "Agent Zero", "chromium-browser" into "Chromium", "chrome-x.com__-Default"
// into "Chrome X".
func titleize(cls string) string {
	s := cls
	// Chrome PWA ids look like "chrome-x.com__-Default": the meaningful part
	// is before the "__" separator.
	if i := strings.Index(s, "__"); i >= 0 {
		s = s[:i]
	}
	tail := s
	if i := strings.LastIndex(tail, "."); i >= 0 {
		tail = tail[i+1:]
	}
	// A tail that is just a TLD or a placeholder ("com", "Default") is not a
	// name — take the leading segment instead ("chrome-x" -> "Chrome X").
	lt := strings.ToLower(tail)
	if tail == "" || lt == "com" || lt == "org" || lt == "net" || lt == "default" || len(tail) <= 2 {
		if i := strings.Index(s, "."); i > 0 {
			tail = s[:i]
			lt = strings.ToLower(tail)
		} else {
			tail = s
			lt = strings.ToLower(tail)
		}
	}
	for _, junk := range []string{"-default", "-browser", "-bin", ".bin"} {
		if strings.HasSuffix(lt, junk) {
			tail = tail[:len(tail)-len(junk)]
			lt = strings.ToLower(tail)
		}
	}
	tail = strings.Trim(tail, "_-. ")
	if tail == "" {
		tail = cls
	}
	words := strings.FieldsFunc(tail, func(r rune) bool {
		return r == ' ' || r == '_' || r == '-'
	})
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func buildAppNameMap() {
	appNameByID = map[string]string{}
	dirs := []string{}
	if d, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(d, ".local", "share", "applications"))
	}
	xdg := os.Getenv("XDG_DATA_DIRS")
	if xdg == "" {
		xdg = "/usr/local/share:/usr/share"
	}
	for _, d := range strings.Split(xdg, ":") {
		if d != "" {
			dirs = append(dirs, filepath.Join(d, "applications"))
		}
	}
	for _, dir := range dirs {
		paths, _ := filepath.Glob(filepath.Join(dir, "*.desktop"))
		for _, p := range paths {
			indexDesktopEntry(p)
		}
	}
}

func indexDesktopEntry(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	id := strings.TrimSuffix(filepath.Base(path), ".desktop")
	var name, wmclass, generic string
	inEntry := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "[") {
			inEntry = line == "[Desktop Entry]"
			continue
		}
		if !inEntry {
			continue
		}
		switch {
		case strings.HasPrefix(line, "Name=") && name == "":
			name = strings.TrimSpace(line[len("Name="):])
		case strings.HasPrefix(line, "GenericName="):
			generic = strings.TrimSpace(line[len("GenericName="):])
		case strings.HasPrefix(line, "StartupWMClass="):
			wmclass = strings.TrimSpace(line[len("StartupWMClass="):])
		}
	}
	if name == "" {
		name = generic
	}
	if name == "" {
		return
	}
	set := func(k string) {
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "" {
			return
		}
		if _, exists := appNameByID[k]; !exists {
			appNameByID[k] = name
		}
	}
	set(id)
	set(wmclass)
	if i := strings.LastIndex(id, "."); i >= 0 {
		set(id[i+1:])
	}
}
