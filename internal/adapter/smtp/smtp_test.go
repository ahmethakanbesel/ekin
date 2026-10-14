package smtp_test

import (
	"bufio"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"

	"kurgu/internal/adapter/smtp"
)

// fakeServer speaks just enough SMTP to accept one message.
func fakeServer(t *testing.T) (int, func() string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var (
		mu  sync.Mutex
		got strings.Builder
	)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		r := bufio.NewReader(conn)
		reply := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		reply("220 fake")
		data := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if data {
				if line == ".\r\n" {
					data = false
					reply("250 ok")
					continue
				}
				mu.Lock()
				got.WriteString(line)
				mu.Unlock()
				continue
			}
			switch cmd := strings.ToUpper(strings.Fields(line)[0]); cmd {
			case "EHLO", "HELO":
				reply("250 fake")
			case "DATA":
				data = true
				reply("354 go")
			case "QUIT":
				reply("221 bye")
				return
			default:
				reply("250 ok")
			}
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().(*net.TCPAddr).Port, func() string { mu.Lock(); defer mu.Unlock(); return got.String() }
}

func TestSend(t *testing.T) {
	port, got := fakeServer(t)
	m := smtp.New(smtp.Config{Host: "127.0.0.1", Port: port, From: "Kurgu <kurgu@example.com>", TLS: "none"})
	if err := m.Send("ada@example.com", "İncelemeye hazır: 2 video", "Merhaba\nİki video hazır."); err != nil {
		t.Fatal(err)
	}
	msg := got()
	if !strings.Contains(msg, "To: <ada@example.com>") || !strings.Contains(msg, "=?utf-8?q?") || !strings.Contains(msg, "İki video hazır.") {
		t.Fatalf("message = %q", msg)
	}
	if err := smtp.New(smtp.Config{}).Send("a@b.c", "x", "y"); err == nil {
		t.Fatal("unconfigured mailer sent")
	}
	_ = strconv.Itoa
}
