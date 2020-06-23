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
	"gorm.io/gorm"
)

var (
	_    gorm.Plugin = &Prometheus{}
	once sync.Once
)

const (
	defaultRefreshInterval = 15   //the prometheus default pull metrics every 15 seconds
	defaultHTTPServerPort  = 9100 // prometheus default pull port
)

type Prometheus struct {
	*gorm.DB
	*DBStats
	*Config
	sync.Once
}

type Config struct {
	DBName          string
	StartServer     bool   // if true, create http server to expose metrics
	HTTPServerPort  uint32 // http server port
	RefreshInterval uint32 // refresh metrics interval.
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

	dbStatsPrefix := "gorm_dbstats_"
	if p.Config.DBName != "" {
		dbStatsPrefix = fmt.Sprintf("%s%s_", dbStatsPrefix, p.Config.DBName)
	}
	p.DBStats = &DBStats{
		MaxOpenConnections: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s%s", dbStatsPrefix, "max_open_connections"),
			Help: "Maximum number of open connections to the database.",
		}),
		OpenConnections: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s%s", dbStatsPrefix, "open_connections"),
			Help: "The number of established connections both in use and idle.",
		}),
		InUse: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s%s", dbStatsPrefix, "in_use"),
			Help: "The number of connections currently in use.",
		}),
		Idle: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s%s", dbStatsPrefix, "idle"),
			Help: "The number of idle connections.",
		}),
		WaitCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s%s", dbStatsPrefix, "wait_count"),
			Help: "The total number of connections waited for.",
		}),
		WaitDuration: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s%s", dbStatsPrefix, "wait_duration"),
			Help: "The total time blocked waiting for a new connection.",
		}),
		MaxIdleClosed: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s%s", dbStatsPrefix, "max_idle_closed"),
			Help: "The total number of connections closed due to SetMaxIdleConns.",
		}),
		MaxLifetimeClosed: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: fmt.Sprintf("%s%s", dbStatsPrefix, "max_lifetime_closed"),
			Help: "The total number of connections closed due to SetConnMaxLifetime.",
		}),
	}

	dbStatsValue := reflect.ValueOf(*p.DBStats)

	for i := 0; i < dbStatsValue.NumField(); i++ {
		_ = prometheus.Register(dbStatsValue.Field(i).Interface().(prometheus.Gauge))
	}

	p.Once.Do(func() {
		if p.Config.StartServer {
			once.Do(func() {
				go p.startServer() //only start once
			})
		}

		go func() {
			for range time.Tick(time.Duration(p.Config.RefreshInterval) * time.Second) {
				p.refresh()
			}
		}()
	})

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

func (p *Prometheus) startServer() {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	err := http.ListenAndServe(fmt.Sprintf(":%d", p.Config.HTTPServerPort), mux)
	if err != nil {
		p.DB.Logger.Error(context.Background(), "gorm:prometheus listen and serve err: ", err)
	}
}
