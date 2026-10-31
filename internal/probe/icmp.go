package probe

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"time"
)

var errProbeTimeout = errors.New("probe timeout")
var pingTimeRegex = regexp.MustCompile(`time[=<]([0-9]+(?:\.[0-9]+)?)\s*ms`)

func probeICMP(ctx context.Context, target string, timeout time.Duration) (float64, error) {
	ms, err := rawICMPPing(ctx, target, timeout)
	if err == nil {
		return ms, nil
	}
	if errors.Is(err, errProbeTimeout) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return 0, errProbeTimeout
	}
	return commandICMPPing(ctx, target, timeout)
}

func rawICMPPing(ctx context.Context, target string, timeout time.Duration) (float64, error) {
	resolver := &net.Resolver{}
	resolveCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ips, err := resolver.LookupIPAddr(resolveCtx, target)
	if err != nil || len(ips) == 0 {
		if errors.Is(resolveCtx.Err(), context.DeadlineExceeded) {
			return 0, errProbeTimeout
		}
		return 0, fmt.Errorf("lookup failed: %w", err)
	}
	ipAddr := ips[0]
	isV6 := ipAddr.IP.To4() == nil

	network := "ip4:icmp"
	listenAddr := "0.0.0.0"
	echoReqType := byte(8)
	echoReplyType := byte(0)
	if isV6 {
		network = "ip6:ipv6-icmp"
		listenAddr = "::"
		echoReqType = 128
		echoReplyType = 129
	}

	conn, err := net.ListenPacket(network, listenAddr)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	var idBuf [4]byte
	_, _ = rand.Read(idBuf[:])
	ident := binary.BigEndian.Uint16(idBuf[0:2])
	seq := binary.BigEndian.Uint16(idBuf[2:4])

	pkt := buildICMPEcho(echoReqType, ident, seq, !isV6)
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)

	start := time.Now()
	if _, err := conn.WriteTo(pkt, &ipAddr); err != nil {
		if isTimeoutError(err) {
			return 0, errProbeTimeout
		}
		return 0, err
	}

	buf := make([]byte, 1500)
	for {
		n, peer, err := conn.ReadFrom(buf)
		if err != nil {
			if isTimeoutError(err) {
				return 0, errProbeTimeout
			}
			return 0, err
		}
		elapsed := time.Since(start).Seconds() * 1000.0
		payload := buf[:n]
		if !isV6 && len(payload) >= 20 && (payload[0]>>4) == 4 {
			ihl := int(payload[0]&0x0f) * 4
			if len(payload) >= ihl+8 {
				payload = payload[ihl:]
			}
		}
		if len(payload) < 8 {
			continue
		}
		if payload[0] != echoReplyType || payload[1] != 0 {
			continue
		}
		rIdent := binary.BigEndian.Uint16(payload[4:6])
		rSeq := binary.BigEndian.Uint16(payload[6:8])
		if rIdent != ident || rSeq != seq {
			continue
		}
		if peerIP, ok := peer.(*net.IPAddr); ok && !peerIP.IP.Equal(ipAddr.IP) {
			continue
		}
		return elapsed, nil
	}
}

func buildICMPEcho(typ byte, ident, seq uint16, calcChecksum bool) []byte {
	pkt := make([]byte, 16)
	pkt[0] = typ
	pkt[1] = 0
	binary.BigEndian.PutUint16(pkt[4:6], ident)
	binary.BigEndian.PutUint16(pkt[6:8], seq)
	copy(pkt[8:], "NOISEPNG")
	if calcChecksum {
		cs := icmpChecksum(pkt)
		binary.BigEndian.PutUint16(pkt[2:4], cs)
	}
	return pkt
}

func icmpChecksum(data []byte) uint16 {
	var sum uint32
	length := len(data)
	for i := 0; i < length-1; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(data[i : i+2]))
	}
	if length%2 == 1 {
		sum += uint32(data[length-1]) << 8
	}
	for (sum >> 16) > 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}

func commandICMPPing(ctx context.Context, target string, timeout time.Duration) (float64, error) {
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	sec := max(1, int(timeout.Seconds()))
	var args []string
	if runtime.GOOS == "windows" {
		args = []string{"-n", "1", "-w", strconv.Itoa(sec * 1000), target}
	} else {
		args = []string{"-n", "-c", "1", "-W", strconv.Itoa(sec), target}
	}

	start := time.Now()
	out, err := exec.CommandContext(pingCtx, "ping", args...).CombinedOutput()
	elapsed := time.Since(start).Seconds() * 1000.0
	if errors.Is(pingCtx.Err(), context.DeadlineExceeded) {
		return 0, errProbeTimeout
	}
	if err != nil {
		if elapsed >= float64(timeout.Milliseconds()) {
			return 0, errProbeTimeout
		}
		return 0, err
	}
	if m := pingTimeRegex.FindSubmatch(out); len(m) == 2 {
		if parsed, err := strconv.ParseFloat(string(m[1]), 64); err == nil {
			return parsed, nil
		}
	}
	return elapsed, nil
}

func isTimeoutError(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}
