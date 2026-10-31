package geoip

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"noise-agent/internal/model"
)

type Placement struct {
	IP     string
	IPv6   string
	Region string
	City   string
}

const defaultUserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

func Resolve(ctx context.Context, cfg model.GeoIPConfig) Placement {
	var p Placement
	ua := strings.TrimSpace(cfg.UserAgent)
	if ua == "" {
		ua = defaultUserAgent
	}

	if cfg.Enabled && cfg.Provider != "" && cfg.Provider != "none" {
		if ip, region, city, ok := lookupProvider(ctx, cfg.Provider, ua); ok {
			if addr, err := netip.ParseAddr(ip); err == nil {
				if addr.Is4() {
					p.IP = addr.String()
				} else if addr.Is6() {
					p.IPv6 = addr.String()
				}
			}
			p.Region = region
			p.City = city
		}
	}

	v4, v6 := localPublicAddrs()
	if p.IP == "" {
		if v4 != "" {
			p.IP = v4
		} else if ip := lookupPlainIP(ctx, "tcp4", ua); ip != "" {
			p.IP = ip
		} else {
			p.IP = outboundIP("udp4", "1.1.1.1:80")
		}
	}
	if p.IPv6 == "" {
		if v6 != "" {
			p.IPv6 = v6
		} else if ip6 := lookupPlainIP(ctx, "tcp6", ua); ip6 != "" {
			p.IPv6 = ip6
		}
	}
	return p
}

func lookupProvider(ctx context.Context, provider, userAgent string) (ip, region, city string, ok bool) {
	var endpoint string
	switch provider {
	case "ipinfo":
		endpoint = "https://ipinfo.io/json"
	case "ip-api":
		endpoint = "http://ip-api.com/json"
	case "ip.sb":
		endpoint = "https://api.ip.sb/geoip"
	default:
		return "", "", "", false
	}

	reqCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", "", "", false
	}
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", "", false
	}

	var raw map[string]any
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&raw); err != nil {
		return "", "", "", false
	}

	str := func(k string) string {
		v, _ := raw[k].(string)
		return strings.TrimSpace(v)
	}

	switch provider {
	case "ipinfo":
		if raw["bogon"] == true || raw["error"] != nil {
			return "", "", "", false
		}
		ip = str("ip")
		region = firstNonEmpty(str("country"), str("region"))
		city = firstNonEmpty(str("city"), str("region"))
	case "ip-api":
		if str("status") == "fail" {
			return "", "", "", false
		}
		ip = str("query")
		region = firstNonEmpty(str("countryCode"), str("regionName"))
		city = firstNonEmpty(str("city"), str("regionName"))
	case "ip.sb":
		ip = str("ip")
		region = firstNonEmpty(str("country_code"), str("region"))
		city = firstNonEmpty(str("city"), str("region"))
	}

	if _, err := netip.ParseAddr(ip); err != nil {
		return "", "", "", false
	}
	return ip, region, city, true
}

func lookupPlainIP(ctx context.Context, network, userAgent string) string {
	dialer := &net.Dialer{Timeout: 4 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	for _, endpoint := range []string{"https://api.ip.sb/ip", "https://ipinfo.io/ip"} {
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, endpoint, nil)
		if err != nil {
			cancel()
			continue
		}
		req.Header.Set("User-Agent", userAgent)
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		resp.Body.Close()
		cancel()
		candidate := strings.TrimSpace(string(body))
		if addr, err := netip.ParseAddr(candidate); err == nil {
			if network == "tcp4" && addr.Is4() {
				return addr.String()
			}
			if network == "tcp6" && addr.Is6() {
				return addr.String()
			}
		}
	}
	return ""
}

func localPublicAddrs() (v4, v6 string) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			addr, ok := netip.AddrFromSlice(ipNet.IP)
			if !ok {
				continue
			}
			addr = addr.Unmap()
			if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLinkLocalUnicast() {
				continue
			}
			if addr.Is4() && v4 == "" {
				v4 = addr.String()
			} else if addr.Is6() && v6 == "" {
				v6 = addr.String()
			}
		}
	}
	return v4, v6
}

func outboundIP(network, target string) string {
	conn, err := net.DialTimeout(network, target, 2*time.Second)
	if err != nil {
		return ""
	}
	defer conn.Close()
	if udpAddr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		if addr, ok := netip.AddrFromSlice(udpAddr.IP); ok {
			return addr.Unmap().String()
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
