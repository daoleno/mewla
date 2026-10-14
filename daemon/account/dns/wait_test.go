package dns

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// fakeNameserver answers TXT queries over UDP with value once ready reports
// true, and with no records before that.
func fakeNameserver(t *testing.T, value string, ready func() bool) string {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	go func() {
		buffer := make([]byte, 512)
		for {
			n, addr, err := conn.ReadFrom(buffer)
			if err != nil {
				return
			}
			var query dnsmessage.Message
			if query.Unpack(buffer[:n]) != nil || len(query.Questions) != 1 {
				continue
			}
			question := query.Questions[0]
			response := dnsmessage.Message{
				Header:    dnsmessage.Header{ID: query.ID, Response: true, Authoritative: true},
				Questions: query.Questions,
			}
			if question.Type == dnsmessage.TypeTXT && ready() {
				response.Answers = []dnsmessage.Resource{{
					Header: dnsmessage.ResourceHeader{Name: question.Name, Type: dnsmessage.TypeTXT, Class: dnsmessage.ClassINET, TTL: 60},
					Body:   &dnsmessage.TXTResource{TXT: []string{value}},
				}}
			}
			packed, err := response.Pack()
			if err == nil {
				_, _ = conn.WriteTo(packed, addr)
			}
		}
	}()
	return conn.LocalAddr().String()
}

func TestTXTWaiterWaitsForEveryNameserver(t *testing.T) {
	var queries atomic.Int32
	early := fakeNameserver(t, "token", func() bool { return true })
	late := fakeNameserver(t, "token", func() bool { return queries.Add(1) > 2 })
	waiter := &TXTWaiter{
		Zone:     "example.test",
		Interval: 10 * time.Millisecond,
		Settle:   time.Millisecond,
		LookupNS: func(context.Context, string) ([]string, error) { return []string{early, late}, nil },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := waiter.Wait(ctx, "_acme-challenge.a.example.test", "token"); err != nil {
		t.Fatal(err)
	}
	if queries.Load() < 3 {
		t.Fatalf("returned after %d queries to the late nameserver", queries.Load())
	}
}

func TestTXTWaiterGivesUpWithTheContext(t *testing.T) {
	server := fakeNameserver(t, "other", func() bool { return true })
	waiter := &TXTWaiter{
		Zone:     "example.test",
		Interval: 10 * time.Millisecond,
		LookupNS: func(context.Context, string) ([]string, error) { return []string{server}, nil },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := waiter.Wait(ctx, "_acme-challenge.a.example.test", "token"); err == nil {
		t.Fatal("waited for a value no nameserver serves")
	}
}
