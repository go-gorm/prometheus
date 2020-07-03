package prometheus

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/push"
	"gorm.io/gorm"
)

var (
	_ gorm.Plugin = &Prometheus{}
)

const (
	defaultRefreshInterval = 15   // the prometheus default pull metrics every 15 seconds
	defaultHTTPServerPort  = 8080 // default pull port
)

type MetricsCollector interface {
	Initialize(map[string]string)
	Set(db *gorm.DB)
	Collector(pusher *push.Pusher) *push.Pusher
}

type Prometheus struct {
	*gorm.DB
	*DBStats
	*Config
	refreshOnce, pushOnce sync.Once
}

type Config struct {
	DBName           string             // use DBName as metrics label
	RefreshInterval  uint32             // refresh metrics interval.
	PushAddr         string             // prometheus pusher address
	StartServer      bool               // if true, create http server to expose metrics
	HTTPServerPort   uint32             // http server port
	MetricsCollector []MetricsCollector // collector
}

func New(config Config) *Prometheus {
	if config.RefreshInterval == 0 {
		config.RefreshInterval = defaultRefreshInterval
	}

	if config.HTTPServerPort == 0 {
		config.HTTPServerPort = defaultHTTPServerPort
	}

	return &Prometheus{Config: &config}
}

func (p *Prometheus) Name() string {
	return "gorm:prometheus"
}

func (p *Prometheus) Initialize(db *gorm.DB) error { //can be called repeatedly
	p.DB = db

	labels := make(map[string]string)
	if p.Config.DBName != "" {
		labels["db_name"] = p.Config.DBName
	}

	p.DBStats = newStats(labels)

	for _, c := range p.Config.MetricsCollector {
		c.Initialize(labels)
	}

	p.refreshOnce.Do(func() {
		go func() {
			for range time.Tick(time.Duration(p.Config.RefreshInterval) * time.Second) {
				p.refresh()
			}
		}()
	})

	if p.Config.StartServer {
		go p.startServer()
	}

	if p.PushAddr != "" {
		go p.startPush()
	}

	return nil
}

func (p *Prometheus) refresh() {
	if db, err := p.DB.DB(); err == nil {
		p.DBStats.Set(db.Stats())
	} else {
		p.DB.Logger.Error(context.Background(), "gorm:prometheus failed to collect db status, got error: %v", err)
	}

	for _, c := range p.MetricsCollector {
		c.Set(p.DB)
	}
}

func (p *Prometheus) startPush() {
	p.pushOnce.Do(func() {
		pusher := push.New(p.PushAddr, p.DBName)

		for _, collector := range p.DBStats.Collectors() {
			pusher = pusher.Collector(collector)
		}

		for _, c := range p.MetricsCollector {
			pusher = c.Collector(pusher)
		}

		for range time.Tick(time.Duration(p.Config.RefreshInterval) * time.Second) {
			err := pusher.Push()
			if err != nil {
				p.DB.Logger.Error(context.Background(), "gorm:prometheus push err: ", err)
			}
		}
	})
}

var httpServerOnce sync.Once

func (p *Prometheus) startServer() {
	httpServerOnce.Do(func() { //only start once
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		err := http.ListenAndServe(fmt.Sprintf(":%d", p.Config.HTTPServerPort), mux)
		if err != nil {
			p.DB.Logger.Error(context.Background(), "gorm:prometheus listen and serve err: ", err)
		}
	})
}
