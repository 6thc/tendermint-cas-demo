package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/6thc/tendermint-cas-demo/internal/cas"
	"github.com/BurntSushi/toml"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/mitchellh/mapstructure"
	"github.com/oklog/run"
	"github.com/peterbourgon/usage"
	"github.com/pkg/errors"
	tendermintabci "github.com/tendermint/tendermint/abci/types"
	tendermintconfig "github.com/tendermint/tendermint/config"
	tendermintlog "github.com/tendermint/tendermint/libs/log"
	tendermintnode "github.com/tendermint/tendermint/node"
	tendermintp2p "github.com/tendermint/tendermint/p2p"
	tendermintprivval "github.com/tendermint/tendermint/privval"
	tendermintproxy "github.com/tendermint/tendermint/proxy"
	tendermintrpcclient "github.com/tendermint/tendermint/rpc/client"
)

func main() {
	fs := flag.NewFlagSet("tendermint-cas-demo", flag.ExitOnError)
	var (
		apiAddr           = fs.String("api-addr", "127.0.0.1:8081", "HTTP API address")
		appFile           = fs.String("app-file", "db.json", "application persistence file")
		appVerbose        = fs.Bool("app-verbose", false, "verbose logging of application information")
		tendermintDir     = fs.String("tendermint-dir", "tendermint", "Tendermint directory (config, data, etc.)")
		tendermintVerbose = fs.Bool("tendermint-verbose", false, "verbose logging of Tendermint information")
	)
	fs.Usage = usage.For(fs, "tendermint-cas-demo [flags]")
	fs.Parse(os.Args[1:])

	var logger log.Logger
	{
		logger = log.NewLogfmtLogger(log.NewSyncWriter(os.Stdout))
	}

	var app tendermintabci.Application
	{
		// Set up the one-shot initial io.Reader for server state.
		var (
			initial io.Reader
			close   = func() error { return nil }
		)
		if f, err := os.Open(*appFile); err == nil {
			initial, close = f, f.Close // actually use the file
		} else if os.IsNotExist(err) {
			// doesn't exist, no problem, don't use it
		} else {
			level.Error(logger).Log("file", *appFile, "during", "Open", "err", err)
			os.Exit(1)
		}

		// Create the app logger.
		appLogger := log.With(logger, "component", "App")
		if !*appVerbose {
			appLogger = level.NewFilter(appLogger, level.AllowInfo()) // info is OK for app
		}

		// Create our ABCI application.
		var err error
		app, err = cas.NewApplication(initial, newSyncWriter(*appFile), appLogger)
		if err != nil {
			level.Error(logger).Log("during", "NewApplicationServer", "err", err)
			os.Exit(1)
		}

		// Close the file we opened for the initial state load (if any).
		if err = close(); err != nil {
			level.Error(logger).Log("file", *appFile, "during", "Close", "err", err)
			os.Exit(1)
		}
	}

	var node *tendermintnode.Node
	{
		// Make sure someone ran `tendermint init`.
		if fi, err := os.Stat(*tendermintDir); os.IsNotExist(err) {
			level.Error(logger).Log("err", "-tendermint-dir missing", "try", "tendermint init --home "+*tendermintDir)
			os.Exit(1)
		} else if err != nil {
			level.Error(logger).Log("err", err, "try", "tendermint init --home "+*tendermintDir)
			os.Exit(1)
		} else if !fi.IsDir() {
			level.Error(logger).Log("err", "-tendermint-dir isn't a directory", "try", "tendermint init --home "+*tendermintDir)
			os.Exit(1)
		}

		// Parse the config file. It's weird that Tendermint doesn't have a
		// helper function for this; they always go through Viper, so I'm just
		// copying that logic, essentially.
		nodeConfig := tendermintconfig.DefaultConfig().SetRoot(*tendermintDir)
		var (
			configFile = filepath.Join(nodeConfig.BaseConfig.RootDir, "config", "config.toml")
			configMap  map[string]interface{} // like viper.AllSettings()
		)
		if _, err := toml.DecodeFile(configFile, &configMap); err != nil {
			level.Error(logger).Log("during", "decode Tendermint config.toml", "err", err)
			os.Exit(1)
		}
		decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
			Result:           nodeConfig,
			WeaklyTypedInput: true,
			DecodeHook:       mapstructure.StringToTimeDurationHookFunc(),
		})
		if err != nil {
			level.Error(logger).Log("during", "build config parser", "err", err)
			os.Exit(1)
		}
		if err := decoder.Decode(configMap); err != nil {
			level.Error(logger).Log("during", "parse config", "err", err)
			os.Exit(1)
		}

		// Gotta load the node key separately, for some reason.
		nodeKey, err := tendermintp2p.LoadOrGenNodeKey(nodeConfig.NodeKeyFile())
		if err != nil {
			level.Error(logger).Log("during", "tendermintp2p.LoadOrGenNodeKey", "err", err)
			os.Exit(1)
		}

		// The Tendermint logger has its own filtering rules.
		tendermintLogger := log.With(logger, "component", "Node")
		if !*tendermintVerbose {
			tendermintLogger = level.NewFilter(tendermintLogger, level.AllowWarn()) // info is too noisy for Tendermint
		}

		// Create the node.
		node, err = tendermintnode.NewNode(
			nodeConfig,
			tendermintprivval.LoadOrGenFilePV(nodeConfig.PrivValidatorFile()),
			nodeKey,
			tendermintproxy.NewLocalClientCreator(app),
			tendermintnode.DefaultGenesisDocProviderFunc(nodeConfig),
			tendermintnode.DefaultDBProvider, // n.b. Tendermint DB, not our state
			tendermintnode.DefaultMetricsProvider(nodeConfig.Instrumentation),
			tendermintAdapter{tendermintLogger},
		)
		if err != nil {
			level.Error(logger).Log("during", "tendermint.DefaultNewNode", "err", err)
			os.Exit(1)
		}
	}

	var api http.Handler
	{
		api = NewCompareAndSwapAPI(tendermintrpcclient.NewLocal(node))
		api = loggingMiddleware{api, log.With(logger, "component", "API")}
	}

	var g run.Group
	{
		g.Add(func() error {
			level.Info(logger).Log("context", "Tendermint node", "addr", node.NodeInfo().ListenAddr)
			if err := node.Start(); err != nil {
				return errors.Wrap(err, "error starting Tendermint node")
			}
			node.Wait()
			return nil
		}, func(error) {
			if err := node.Stop(); err != nil {
				level.Error(logger).Log("during", "node.Stop", "err", err)
			}
		})
	}
	{
		server := &http.Server{
			Addr:    *apiAddr,
			Handler: api,
		}
		g.Add(func() error {
			level.Info(logger).Log("context", "CompareAndSwap API", "addr", *apiAddr)
			return server.ListenAndServe()
		}, func(error) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			server.Shutdown(ctx)
		})
	}
	{
		ctx, cancel := context.WithCancel(context.Background())
		g.Add(func() error {
			c := make(chan os.Signal, 1)
			signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
			select {
			case sig := <-c:
				return fmt.Errorf("received signal %s", sig)
			case <-ctx.Done():
				return ctx.Err()
			}
		}, func(error) {
			cancel()
		})
	}
	level.Info(logger).Log("exit", g.Run())
}

type tendermintAdapter struct{ log.Logger }

func (a tendermintAdapter) Debug(msg string, keyvals ...interface{}) {
	level.Debug(log.With(a.Logger, keyvals...)).Log("msg", msg)
}

func (a tendermintAdapter) Info(msg string, keyvals ...interface{}) {
	level.Info(log.With(a.Logger, keyvals...)).Log("msg", msg)
}

func (a tendermintAdapter) Error(msg string, keyvals ...interface{}) {
	level.Error(log.With(a.Logger, keyvals...)).Log("msg", msg)
}

func (a tendermintAdapter) With(keyvals ...interface{}) tendermintlog.Logger {
	return tendermintAdapter{log.With(a.Logger, keyvals...)}
}

type syncWriter struct {
	filename string
	f        *os.File
}

func newSyncWriter(filename string) io.WriteCloser {
	return &syncWriter{
		filename: filename,
	}
}

func (w *syncWriter) Write(p []byte) (int, error) {
	if w.f == nil {
		f, err := os.Create(w.filename)
		if err != nil {
			return 0, err
		}
		w.f = f
	}
	return w.f.Write(p)
}

func (w *syncWriter) Close() error {
	defer func() { w.f = nil }()
	return w.f.Close()
}
