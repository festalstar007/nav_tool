package main

import (
	"context"
	"flag"
	"log"
	"nav_tool/internal/app"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"
)

func main() {
	log.SetFlags(log.LstdFlags)
	if len(os.Args) > 1 && os.Args[1] == "restore" {
		fs := flag.NewFlagSet("restore", flag.ExitOnError)
		archive := fs.String("archive", "", "backup archive")
		target := fs.String("to", "", "new data directory")
		fs.Parse(os.Args[2:])
		if *archive == "" || *target == "" {
			log.Fatal("use restore -archive backup.tar.gz -to NEW_DIRECTORY")
		}
		if e := app.Restore(*archive, *target); e != nil {
			log.Fatal(e)
		}
		log.Print("restore completed; sessions cleared")
		return
	}
	data := flag.String("data", env("NAV_DATA_DIR", "data"), "data directory")
	web := flag.String("web", env("NAV_WEB_DIR", "web/dist"), "static frontend directory")
	addr := flag.String("addr", env("NAV_ADDR", "127.0.0.1:31506"), "listen address")
	backup := flag.Bool("backup", false, "create one local backup and exit")
	importFile := flag.String("import", "", "import browser HTML while the service is stopped, then exit")
	flag.Parse()
	origin, e := app.ParseOrigin(os.Getenv("NAV_ORIGIN"))
	if e != nil {
		log.Fatal(e)
	}
	host, _, e := net.SplitHostPort(*addr)
	if e != nil {
		log.Fatal(e)
	}
	if !*backup && origin == "" && host != "127.0.0.1" && host != "::1" && host != "localhost" {
		log.Fatal("NAV_ORIGIN is required for non-loopback listening")
	}
	a, e := app.New(app.Config{DataDir: *data, WebDir: *web, Origin: origin, SecureCookie: strings.HasPrefix(origin, "https://")})
	if e != nil {
		log.Fatal(e)
	}
	defer a.Close()
	if *importFile != "" {
		added, duplicates, err := a.ImportFile(*importFile)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("import completed: %d added, %d duplicates skipped", added, duplicates)
		return
	}
	if *backup {
		name, e := a.Backup()
		if e != nil {
			log.Fatal(e)
		}
		log.Print("backup created: ", name)
		return
	}
	if _, e = os.Stat(filepath.Join(*data, "setup-token")); e == nil {
		log.Print("first-run setup credential is in ", filepath.Join(*data, "setup-token"))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go a.Scheduler(ctx)
	srv := &http.Server{Addr: *addr, Handler: a.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 2 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		srv.Shutdown(c)
	}()
	log.Print("nav_tool listening on http://", *addr)
	if e = srv.ListenAndServe(); e != nil && e != http.ErrServerClosed {
		log.Fatal(e)
	}
}
func env(k, fallback string) string {
	if s := os.Getenv(k); s != "" {
		return s
	}
	return fallback
}
