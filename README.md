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
  DBName:          "db1", // use `DBName` as metrics label
  RefreshInterval: 15,    // refresh metrics interval (seconds)
  StartServer:     true,  // start http server to expose metrics
  HTTPServerPort:  8080,  // http server port
  PushAddr:        "prometheus pusher address", // push metrics if `PushAddr` configured
}))
```
