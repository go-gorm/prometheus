# Prometheus

Collect DB Status with Prometheus

## Usage

```go
import (
  "gorm.io/gorm"
  "gorm.io/driver/sqlite"
  "gorm.io/plugin/prometheus"
)

db, err := gorm.Open(sqlite.Open("gorm.db"), &gorm.Config{})

db.Use(prometheus.New(prometheus.Config{
  StartServer:     true, // start http server to expose metrics
  HTTPServerPort:  9100, // http server port
  RefreshInterval: 15,   // refresh metrics interval (seconds)
}))
```
