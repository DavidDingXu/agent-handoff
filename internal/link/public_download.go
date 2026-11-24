package link

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// DownloadPublicURL loads an encrypted handoff link from a public HTTPS
// endpoint. A dedicated transport checks the address actually selected by the
// dialer, including after a redirect or changes in DNS.
func DownloadPublicURL(rawURL string) ([]byte, string, error) {
	if _, err := validatePublicHTTPSURL(rawURL, "share URL"); err != nil {
		return nil, "", err
	}
	dialer := net.Dialer{Timeout: 30 * time.Second, Control: checkPublicDownloadAddress}
	transport := &http.Transport{DialContext: dialer.DialContext, ForceAttemptHTTP2: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 120 * time.Second}
	return DownloadURL(rawURL, client)
}

var sharedIPv4 = netip.MustParsePrefix("100.64.0.0/10")

func checkPublicDownloadAddress(_ string, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	ip = ip.Unmap()
	if err != nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || sharedIPv4.Contains(ip) {
		return fmt.Errorf("share endpoint must resolve to a public address")
	}
	return nil
}
