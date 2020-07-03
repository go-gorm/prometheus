package prometheus

import (
	"context"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"
	"gorm.io/gorm"
	"strconv"
)

const statusPrefix = "gorm_status_"

type Mysql struct {
	StatusVariableName []string
	status             map[string]prometheus.Gauge
}

func (m *Mysql) Initialize(label map[string]string) {
	m.status = make(map[string]prometheus.Gauge)
	for _, v := range m.StatusVariableName {
		m.status[v] = prometheus.NewGauge(prometheus.GaugeOpts{
			Name:        statusPrefix + v,
			ConstLabels: label,
		})
	}

	for _, gauge := range m.status {
		_ = prometheus.Register(gauge)
	}
}

func (m *Mysql) Set(db *gorm.DB) {
	sqlDb, _ := db.DB()
	rows, err := sqlDb.Query("SHOW STATUS")
	if err != nil {
		db.Logger.Error(context.Background(), "gorm:prometheus query error: %v", err)
	}

	var variableName, variableValue string
	for rows.Next() {
		err = rows.Scan(&variableName, &variableValue)
		if err != nil {
			db.Logger.Error(context.Background(), "gorm:prometheus scan got error: %v", err)
			continue
		}

		for _, v := range m.StatusVariableName {
			if v == variableName {
				value, err := strconv.ParseFloat(variableValue, 64)
				if err != nil {
					db.Logger.Error(context.Background(), "gorm:prometheus parse float got error: %v", err)
					continue
				}

				if _, ok := m.status[v]; ok {
					m.status[v].Set(value)
				}
			}
		}
	}
}

func (m *Mysql) Collector(pusher *push.Pusher) *push.Pusher {
	if len(m.status) != 0 {
		for _, gauge := range m.status {
			pusher = pusher.Collector(gauge)
		}
	}

	return pusher
}
