//go:build darwin

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"time"
)

// VsockProxy proxies HTTP requests to lokad inside the VM.
// It uses the VM's NAT IP (with vsock fallback) to reach the guest.
type VsockProxy struct {
	vm     *VM
	proxy  *httputil.ReverseProxy
	logger *slog.Logger
}

// NewVsockProxy creates an HTTP reverse proxy that forwards to lokad in the VM.
func NewVsockProxy(vm *VM, logger *slog.Logger) *VsockProxy {
	p := &VsockProxy{vm: vm, logger: logger}

	// Custom transport that dials the VM guest instead of resolving the host.
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return vm.DialGuest(6840)
		},
		MaxIdleConns:        100,
		IdleConnTimeout:     90 * time.Second,
		MaxIdleConnsPerHost: 50,
	}

	p.proxy = &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = fmt.Sprintf("%s:6840", vm.GuestIP())
		},
		Transport: transport,
	}

	return p
}

// ServeHTTP forwards the request to lokad inside the VM.
func (p *VsockProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.proxy.ServeHTTP(w, r)
}

// startTCPProxy creates a raw TCP relay from hostAddr to the VM guest on guestPort.
// Used for gRPC which needs raw TCP passthrough.
func startTCPProxy(hostAddr string, vm *VM, guestPort int, logger *slog.Logger) {
	listener, err := net.Listen("tcp", hostAddr)
	if err != nil {
		logger.Error("TCP proxy listen failed", "addr", hostAddr, "error", err)
		return
	}
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go relayConn(conn, vm, guestPort, logger)
	}
}

// relayConn connects a client connection to the VM guest and copies data bidirectionally.
func relayConn(clientConn net.Conn, vm *VM, guestPort int, logger *slog.Logger) {
	defer clientConn.Close()

	vmConn, err := vm.DialGuest(guestPort)
	if err != nil {
		logger.Debug("guest dial failed", "port", guestPort, "error", err)
		return
	}
	defer vmConn.Close()

	// Bidirectional relay.
	done := make(chan struct{})
	go func() {
		io.Copy(vmConn, clientConn)
		close(done)
	}()
	io.Copy(clientConn, vmConn)
	<-done
}
