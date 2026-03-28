// loka-proxy is a tiny reverse proxy that listens on ports 80 and 443
// and forwards all traffic to the LOKA domain proxy on port 6843.
// It runs as root (started via osascript) to bind privileged ports.
package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"net"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

func main() {
	target := flag.String("target", "127.0.0.1:6843", "Target address to proxy to")
	httpPort := flag.Int("http", 80, "HTTP listen port")
	httpsPort := flag.Int("https", 443, "HTTPS listen port")
	certFile := flag.String("cert", "", "TLS cert file (for HTTPS)")
	keyFile := flag.String("key", "", "TLS key file (for HTTPS)")
	pidFile := flag.String("pid", "", "Write PID to this file")
	flag.Parse()

	if *pidFile != "" {
		os.WriteFile(*pidFile, []byte(fmt.Sprintf("%d", os.Getpid())), 0o644)
		defer os.Remove(*pidFile)
	}

	var wg sync.WaitGroup

	// HTTP listener (port 80)
	wg.Add(1)
	go func() {
		defer wg.Done()
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *httpPort))
		if err != nil {
			fmt.Fprintf(os.Stderr, "listen :%d: %v\n", *httpPort, err)
			return
		}
		defer ln.Close()
		fmt.Printf("proxy :%d → %s\n", *httpPort, *target)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go proxy(conn, *target)
		}
	}()

	// HTTPS listener (port 443) — auto-reloads cert from disk on each handshake
	if *certFile != "" && *keyFile != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cf, kf := *certFile, *keyFile
			tlsCfg := &tls.Config{
				GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
					cert, err := tls.LoadX509KeyPair(cf, kf)
					if err != nil {
						return nil, err
					}
					return &cert, nil
				},
			}
			ln, err := tls.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *httpsPort), tlsCfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "listen :%d: %v\n", *httpsPort, err)
				return
			}
			defer ln.Close()
			fmt.Printf("proxy :%d → %s (TLS)\n", *httpsPort, *target)
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				go proxy(conn, *target)
			}
		}()
	}

	// Wait for signal
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	<-sig
}

func proxy(src net.Conn, target string) {
	defer src.Close()
	dst, err := net.Dial("tcp", target)
	if err != nil {
		return
	}
	defer dst.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); io.Copy(dst, src) }()
	go func() { defer wg.Done(); io.Copy(src, dst) }()
	wg.Wait()
}
