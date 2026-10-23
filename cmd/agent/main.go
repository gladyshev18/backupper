// backupper-agent runs on the server: it keeps the backup schedule and
// streams dumps straight to the clouds without storing them on disk.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"backupper/internal/agent"
	ver "backupper/internal/version"
)

var version = ver.Version

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	confPath := env("BACKUPPER_CONFIG", agent.DefaultConf)
	dataDir := env("BACKUPPER_DATA", agent.DefaultData)
	if len(os.Args) < 2 {
		fmt.Println("usage: backupper-agent serve|install|uninstall|token|version")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "version":
		fmt.Println(version)
	case "serve":
		serve(confPath, dataDir)
	case "install":
		fs := flag.NewFlagSet("install", flag.ExitOnError)
		port := fs.Int("port", agent.DefaultPort, "local API port")
		name := fs.String("name", "", "server name shown in the panel and used in cloud paths")
		fs.Parse(os.Args[2:])
		res, err := agent.Install(confPath, *port, *name, version)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		agent.PrintJSON(res)
	case "uninstall":
		fs := flag.NewFlagSet("uninstall", flag.ExitOnError)
		purge := fs.Bool("purge", false, "also remove config and history")
		fs.Parse(os.Args[2:])
		if err := agent.Uninstall(confPath, *purge); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "token":
		cfg, err := agent.LoadConfig(confPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		agent.PrintJSON(agent.InstallResult{Token: cfg.Token, Listen: cfg.Listen, Version: version})
	default:
		fmt.Fprintln(os.Stderr, "unknown command", os.Args[1])
		os.Exit(2)
	}
}

func serve(confPath, dataDir string) {
	state, err := agent.OpenState(agent.Paths{Config: confPath, DataDir: dataDir})
	if err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a := agent.New(state, version)
	a.Start(ctx)

	listen := state.Config().Listen
	if listen == "" {
		listen = fmt.Sprintf("127.0.0.1:%d", agent.DefaultPort)
	}
	srv := &http.Server{Addr: listen, Handler: a.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	log.Printf("backupper-agent %s listening on %s", version, listen)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
	state.SaveHistory()
}
