// Package frpc renders frpc.toml from a tunnel config and manages the frpc
// child process (start / reload / restart / status).
package frpc

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
)

// Proxy is one frpc proxy definition.
type Proxy struct {
	Name       string
	Type       string // "http" or "tcp"
	LocalIP    string
	LocalPort  int
	Domain     string // http only
	RemotePort int    // tcp only
}

// RenderInput is everything needed to produce a frpc.toml.
type RenderInput struct {
	ServerAddr string
	ServerPort int
	AuthToken  string
	Proxies    []Proxy
}

var (
	nameSanitize = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
	hostnameRe   = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)
)

// Render produces a stable, readable frpc.toml. Invalid or disabled proxies are
// expected to be filtered by the caller; Render additionally sanitizes proxy
// names and skips entries that fail basic validation, returning the skipped
// reasons as warnings (never fatal for a single bad proxy).
func Render(in RenderInput) (string, []string, error) {
	if in.ServerAddr == "" {
		return "", nil, fmt.Errorf("server_addr is empty")
	}
	if in.ServerPort < 1 || in.ServerPort > 65535 {
		return "", nil, fmt.Errorf("server_port %d is invalid", in.ServerPort)
	}

	// Sort by name for deterministic output across syncs.
	proxies := append([]Proxy(nil), in.Proxies...)
	sort.SliceStable(proxies, func(i, j int) bool { return proxies[i].Name < proxies[j].Name })

	var b strings.Builder
	fmt.Fprintf(&b, "serverAddr = %s\n", quote(in.ServerAddr))
	fmt.Fprintf(&b, "serverPort = %d\n\n", in.ServerPort)
	b.WriteString("auth.method = \"token\"\n")
	fmt.Fprintf(&b, "auth.token = %s\n", quote(in.AuthToken))

	var warnings []string
	seen := map[string]bool{}
	for _, p := range proxies {
		name := sanitizeName(p.Name)
		if name == "" {
			warnings = append(warnings, fmt.Sprintf("skip proxy with empty name (was %q)", p.Name))
			continue
		}
		if seen[name] {
			warnings = append(warnings, fmt.Sprintf("skip duplicate proxy name %q", name))
			continue
		}
		if !validHost(p.LocalIP) {
			warnings = append(warnings, fmt.Sprintf("skip proxy %q: invalid local host %q", name, p.LocalIP))
			continue
		}
		if p.LocalPort < 1 || p.LocalPort > 65535 {
			warnings = append(warnings, fmt.Sprintf("skip proxy %q: invalid local port %d", name, p.LocalPort))
			continue
		}

		switch p.Type {
		case "http":
			if !validDomain(p.Domain) {
				warnings = append(warnings, fmt.Sprintf("skip http proxy %q: invalid domain %q", name, p.Domain))
				continue
			}
			b.WriteString("\n[[proxies]]\n")
			fmt.Fprintf(&b, "name = %s\n", quote(name))
			b.WriteString("type = \"http\"\n")
			fmt.Fprintf(&b, "localIP = %s\n", quote(p.LocalIP))
			fmt.Fprintf(&b, "localPort = %d\n", p.LocalPort)
			fmt.Fprintf(&b, "customDomains = [%s]\n", quote(p.Domain))
		case "tcp":
			if p.RemotePort < 1 || p.RemotePort > 65535 {
				warnings = append(warnings, fmt.Sprintf("skip tcp proxy %q: invalid remote port %d", name, p.RemotePort))
				continue
			}
			b.WriteString("\n[[proxies]]\n")
			fmt.Fprintf(&b, "name = %s\n", quote(name))
			b.WriteString("type = \"tcp\"\n")
			fmt.Fprintf(&b, "localIP = %s\n", quote(p.LocalIP))
			fmt.Fprintf(&b, "localPort = %d\n", p.LocalPort)
			fmt.Fprintf(&b, "remotePort = %d\n", p.RemotePort)
		default:
			warnings = append(warnings, fmt.Sprintf("skip proxy %q: unsupported type %q", name, p.Type))
			continue
		}
		seen[name] = true
	}

	return b.String(), warnings, nil
}

// sanitizeName keeps only [a-zA-Z0-9_-], matching frp's allowed proxy names.
func sanitizeName(name string) string {
	return strings.Trim(nameSanitize.ReplaceAllString(strings.TrimSpace(name), "-"), "-")
}

func validHost(h string) bool {
	h = strings.TrimSpace(h)
	if h == "" {
		return false
	}
	if net.ParseIP(h) != nil {
		return true
	}
	return hostnameRe.MatchString(h)
}

func validDomain(d string) bool {
	d = strings.TrimSpace(d)
	if !strings.Contains(d, ".") {
		return false
	}
	return hostnameRe.MatchString(d)
}

// quote renders a TOML basic string with the minimal required escaping.
func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`)
	return `"` + r.Replace(s) + `"`
}
