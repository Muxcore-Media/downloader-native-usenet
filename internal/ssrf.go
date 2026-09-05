package internal

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
)

func classifyNZBURL(raw string, allowPrivate bool) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid nzb url: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("unsupported nzb url scheme %q (want http or https)", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("nzb url missing host")
	}
	if allowPrivate {
		return nil
	}
	return blockFetchHost(context.Background(), u.Hostname())
}

func blockFetchHost(ctx context.Context, host string) error {
	host = strings.TrimSpace(strings.TrimSuffix(host, "."))
	if host == "" {
		return fmt.Errorf("nzb url missing host")
	}
	lower := strings.ToLower(host)
	if isMetadataHost(lower) {
		return fmt.Errorf("nzb url host %q is blocked (metadata)", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("nzb url host %q is blocked (private/loopback)", host)
		}
		return nil
	}
	addrs, err := net.DefaultResolver.LookupHost(ctx, host)
	if err != nil {
		return fmt.Errorf("nzb url host lookup failed for %q: %w", host, err)
	}
	for _, a := range addrs {
		if ip := net.ParseIP(a); ip != nil && isBlockedIP(ip) {
			return fmt.Errorf("nzb url host %q resolves to blocked IP %q", host, a)
		}
	}
	return nil
}

func isMetadataHost(host string) bool {
	switch host {
	case "metadata.google.internal", "metadata.goog":
		return true
	}
	return strings.HasPrefix(host, "169.254.")
}

func isBlockedIP(ip net.IP) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsLinkLocalMulticast()
}
