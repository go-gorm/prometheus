package prometheus

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
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

type Prometheus struct {
	*gorm.DB
	*DBStats
	*Config
	refreshOnce, pushOnce sync.Once
}

type Config struct {
	DBName          string // use DBName as metrics label
	RefreshInterval uint32 // refresh metrics interval.
	PushAddr        string // prometheus pusher address
	StartServer     bool   // if true, create http server to expose metrics
	HTTPServerPort  uint32 // http server port
}

type DBStats struct {
	MaxOpenConnections prometheus.Gauge // Maximum number of open connections to the database.

	// Pool Status
	OpenConnections prometheus.Gauge // The number of established connections both in use and idle.
	InUse           prometheus.Gauge // The number of connections currently in use.
	Idle            prometheus.Gauge // The number of idle connections.

	// Counters
	WaitCount         prometheus.Gauge // The total number of connections waited for.
	WaitDuration      prometheus.Gauge // The total time blocked waiting for a new connection.
	MaxIdleClosed     prometheus.Gauge // The total number of connections closed due to SetMaxIdleConns.
	MaxLifetimeClosed prometheus.Gauge // The total number of connections closed due to SetConnMaxLifetime.
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
	p.DBStats = &DBStats{
		MaxOpenConnections: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        "gorm_dbstats_max_open_connections",
			Help:        "Maximum number of open connections to the database.",
			ConstLabels: labels,
		}),
		OpenConnections: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        "gorm_dbstats_open_connections",
			Help:        "The number of established connections both in use and idle.",
			ConstLabels: labels,
		}),
		InUse: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        "gorm_dbstats_in_use",
			Help:        "The number of connections currently in use.",
			ConstLabels: labels,
		}),
		Idle: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        "gorm_dbstats_idle",
			Help:        "The number of idle connections.",
			ConstLabels: labels,
		}),
		WaitCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        "gorm_dbstats_wait_count",
			Help:        "The total number of connections waited for.",
			ConstLabels: labels,
		}),
		WaitDuration: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        "gorm_dbstats_wait_duration",
			Help:        "The total time blocked waiting for a new connection.",
			ConstLabels: labels,
		}),
		MaxIdleClosed: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        "gorm_dbstats_max_idle_closed",
			Help:        "The total number of connections closed due to SetMaxIdleConns.",
			ConstLabels: labels,
		}),
		MaxLifetimeClosed: prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        "gorm_dbstats_max_lifetime_closed",
			Help:        "The total number of connections closed due to SetConnMaxLifetime.",
			ConstLabels: labels,
		}),
	}

	dbStatsValue := reflect.ValueOf(*p.DBStats)

	for i := 0; i < dbStatsValue.NumField(); i++ {
		_ = prometheus.Register(dbStatsValue.Field(i).Interface().(prometheus.Gauge))
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
		dbStats := db.Stats()

		p.DBStats.MaxOpenConnections.Set(float64(dbStats.MaxOpenConnections))
		p.DBStats.OpenConnections.Set(float64(dbStats.OpenConnections))
		p.DBStats.InUse.Set(float64(dbStats.InUse))
		p.DBStats.Idle.Set(float64(dbStats.Idle))
		p.DBStats.WaitCount.Set(float64(dbStats.WaitCount))
		p.DBStats.WaitDuration.Set(float64(dbStats.WaitDuration))
		p.DBStats.MaxIdleClosed.Set(float64(dbStats.MaxIdleClosed))
		p.DBStats.MaxLifetimeClosed.Set(float64(dbStats.MaxLifetimeClosed))
	} else {
		p.DB.Logger.Error(context.Background(), "gorm:prometheus failed to collect db status, got error: %v", err)
	}
}

func (p *Prometheus) startPush() {
	p.pushOnce.Do(func() {
		pusher := push.New(p.PushAddr, "gorm")

		dbStatsValue := reflect.ValueOf(*p.DBStats)
		for i := 0; i < dbStatsValue.NumField(); i++ {
			pusher = pusher.Collector(dbStatsValue.Field(i).Interface().(prometheus.Gauge))
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
