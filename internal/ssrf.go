package internal

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Muxcore-Media/core/sdk/go/module/netguard"
)

// nzbGuardOptions is UserURL when private hosts are refused, and Integration
// when the operator opts into LAN NZB URLs. Integration still refuses
// link-local and cloud metadata. The old allowPrivate path returned before
// those checks.
func nzbGuardOptions(allowPrivate bool) (netguard.Profile, netguard.Options) {
	opts := netguard.Options{Timeout: 2 * time.Minute}
	if !allowPrivate {
		return netguard.UserURL, opts
	}
	opts.AllowPrivate = true
	opts.AllowLoopback = true
	return netguard.Integration, opts
}

func newGuardedClient(allowPrivate bool) *http.Client {
	profile, opts := nzbGuardOptions(allowPrivate)
	return netguard.NewClient(profile, opts)
}

func classifyNZBURL(raw string, allowPrivate bool) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("nzb url missing host")
	}
	profile, opts := nzbGuardOptions(allowPrivate)
	if err := netguard.ValidateURL(raw, profile, opts); err != nil {
		return fmt.Errorf("nzb url: %w", err)
	}
	return nil
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
