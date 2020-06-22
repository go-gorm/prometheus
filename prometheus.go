package prometheus

import (
	"fmt"
	"github.com/jinzhu/gorm"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
	"reflect"
	"sync"
	"time"
)

type Option struct {
	HttpServer      bool   // if true, create http server to expose metrics
	HttpServerPort  uint32 // http server port
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

const (
	defaultRefreshInterval = 15   //the prometheus default pull metrics every 15 seconds
	DefaultHttpServerPort  = 9100 // prometheus default pull port
)

var (
	stats  *DBStats
	option Option
	db     *gorm.DB
	once   sync.Once
)

func New(o Option) {
	option = o

	if option.HttpServer == true {
		httpServer()
	}

	stats = &DBStats{
		MaxOpenConnections: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "max_open_connections",
			Help: "Maximum number of open connections to the database.",
		}),
		OpenConnections: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "open_connections",
			Help: "The number of established connections both in use and idle.",
		}),
		InUse: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "in_use",
			Help: "The number of connections currently in use.",
		}),
		Idle: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "idle",
			Help: "The number of idle connections.",
		}),
		WaitCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "wait_count",
			Help: "The total number of connections waited for.",
		}),
		WaitDuration: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "wait_duration",
			Help: "The total time blocked waiting for a new connection.",
		}),
		MaxIdleClosed: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "max_idle_closed",
			Help: "The total number of connections closed due to SetMaxIdleConns.",
		}),
		MaxLifetimeClosed: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "max_lifetime_closed",
			Help: "The total number of connections closed due to SetConnMaxLifetime.",
		}),
	}

	dbStatsValue := reflect.ValueOf(stats)

	for i := 0; i < dbStatsValue.NumField(); i++ {
		_ = prometheus.Register(dbStatsValue.Field(i).Interface().(prometheus.Gauge))
	}
}

func (dbStats *DBStats) Name() string {
	return "gorm prometheus plugin"
}

func (dbStats *DBStats) Initialize(database *gorm.DB) { //can be called repeatedly
	db = database

	once.Do(func() {
		go func() {
			var interval uint32
			if option.RefreshInterval != 0 {
				interval = option.RefreshInterval
			} else {
				interval = defaultRefreshInterval
			}

			ticker := time.NewTicker(time.Duration(interval) * time.Second)
			for {
				<-ticker.C
				refresh()
			}
		}()
	})
}

func refresh() {
	dbStats := db.DB().Stats()

	stats.MaxOpenConnections.Set(float64(dbStats.MaxOpenConnections))
	stats.OpenConnections.Set(float64(dbStats.OpenConnections))
	stats.InUse.Set(float64(dbStats.InUse))
	stats.Idle.Set(float64(dbStats.Idle))
	stats.WaitCount.Set(float64(dbStats.WaitCount))
	stats.WaitDuration.Set(float64(dbStats.WaitDuration))
	stats.MaxIdleClosed.Set(float64(dbStats.MaxIdleClosed))
	stats.MaxLifetimeClosed.Set(float64(dbStats.MaxLifetimeClosed))
}

func httpServer() {
	var port uint32
	if option.HttpServerPort != 0 {
		port = option.HttpServerPort
	} else {
		port = DefaultHttpServerPort
	}

	go func() {
		http.Handle("/metrics", promhttp.Handler())
		err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
		if err != nil {
			fmt.Println("gorm plugin prometheus listen and serve err: ", err)
		}
	}()
}
