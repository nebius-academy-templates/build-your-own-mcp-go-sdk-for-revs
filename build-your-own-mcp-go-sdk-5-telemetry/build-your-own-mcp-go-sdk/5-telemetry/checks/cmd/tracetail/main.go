// Command tracetail prints snapshots of local OTLP traces across export batches.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/nebius-academy-templates/build-your-own-mcp-go-sdk/5-telemetry/checks/internal/receiver"
)

func main() {
	port := flag.Int("port", 4318, "loopback OTLP port")
	flag.Parse()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	r := &receiver.Receiver{}
	srv := &http.Server{Handler: r, ReadHeaderTimeout: 5 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, err)
			stop()
		}
	}()
	fmt.Printf("OTLP receiver: http://%s/v1/traces (Ctrl-C to stop)\n", ln.Addr())
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	count := -1
	show := func() {
		rows := r.Snapshot()
		if len(rows) == count {
			return
		}
		count = len(rows)
		if count == 0 {
			return
		}
		fmt.Printf("\nSnapshot: %d spans, buffered across batches\n", count)
		byID := map[string]receiver.Record{}
		children := map[string][]receiver.Record{}
		key := func(row receiver.Record) string {
			return receiver.ID(row.Span.TraceId) + ":" + receiver.ID(row.Span.SpanId)
		}
		for _, row := range rows {
			byID[key(row)] = row
			p := receiver.ID(row.Span.TraceId) + ":" + receiver.ID(row.Span.ParentSpanId)
			children[p] = append(children[p], row)
		}
		seen := map[string]bool{}
		var printRow func(receiver.Record, int)
		printRow = func(row receiver.Record, depth int) {
			k := key(row)
			if seen[k] {
				return
			}
			seen[k] = true
			s := row.Span
			fmt.Printf("%s%s %s %.3f ms trace=%s span=%s parent=%s status=%s attrs=%v resource=%v\n", strings.Repeat("  ", depth), s.Name, s.Kind, float64(s.EndTimeUnixNano-s.StartTimeUnixNano)/1e6, receiver.ID(s.TraceId), receiver.ID(s.SpanId), receiver.ID(s.ParentSpanId), s.GetStatus().GetCode(), receiver.Attributes(s.Attributes), row.Resource)
			for _, child := range children[k] {
				printRow(child, depth+1)
			}
		}
		for _, row := range rows {
			p := receiver.ID(row.Span.TraceId) + ":" + receiver.ID(row.Span.ParentSpanId)
			if _, found := byID[p]; !found {
				if len(row.Span.ParentSpanId) > 0 {
					fmt.Printf("External or not-yet-received parent: %s\n", p)
				}
				printRow(row, 0)
			}
		}
		for _, row := range rows {
			printRow(row, 0)
		}
	}
	for {
		select {
		case <-ticker.C:
			show()
		case <-ctx.Done():
			flush, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := srv.Shutdown(flush); err != nil {
				fmt.Fprintln(os.Stderr, err)
			}
			cancel()
			show()
			return
		}
	}
}
