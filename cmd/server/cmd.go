package main

import (
	"fmt"
	"github.com/PutskouDzmitry/DbTr/pkg/api"
	"github.com/PutskouDzmitry/DbTr/pkg/const_db"
	"github.com/PutskouDzmitry/DbTr/pkg/data"
	"github.com/PutskouDzmitry/DbTr/pkg/db"
	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	host       = os.Getenv("POSTGRES_HOST_SERVER")
	port       = os.Getenv("POSTGRES_PORT_SERVER")
	user       = os.Getenv("POSTGRES_USER_SERVER")
	dbname     = os.Getenv("POSTGRES_DB_NAME_SERVER")
	password   = os.Getenv("POSTGRES_PASSWORD_SERVER")
	sslmode    = os.Getenv("POSTGRES_SSLMODE_SERVER")
	portServer = os.Getenv("SERVER_OUT_PORT")
)

func initializationVar() {
	if host == "" {
		host = const_db.Host
	}
	if port == "" {
		port = const_db.Port
	}
	if user == "" {
		user = const_db.User
	}
	if dbname == "" {
		dbname = const_db.DbName
	}
	if password == "" {
		password = const_db.Password
	}
	if sslmode == "" {
		sslmode = const_db.Sslmode
	}
	if portServer == "" {
		portServer = "8081"
	}
}

func initializationPrometheus() {
	prometheus.Register(totalRequests)
	prometheus.Register(responseStatus)
	prometheus.Register(httpDuration)
}

func main() {
	initializationVar()
	initializationPrometheus()
	conn, err := ping()
	if err != nil {
		logrus.Fatal(err)
	}
	// 2. create router that allows to set routes
	r := mux.NewRouter()
	r.Use(prometheusMiddleware)

	// Prometheus endpoint
	r.Path("/prometheus").Handler(promhttp.Handler())

	// Serving static files
	r.PathPrefix("/").Handler(http.FileServer(http.Dir("./static/")))
	// 3. connect to data layer
	userData := data.NewBookData(conn)
	// 4. send data layer to api layer
	api.ServeUserResource(r, *userData)
	// 5. cors for making requests from any domain
	r.Use(mux.CORSMethodMiddleware(r))
	// 6. start server
	listener, err := net.Listen("tcp", fmt.Sprint(":"+portServer))
	if err != nil {
		log.Fatal("Server Listen port...", err)
	}
	if err := http.Serve(listener, r); err != nil {
		log.Fatal("Server has been crashed...")
	}
}

func ping() (*gorm.DB, error) {
	var conn *gorm.DB
	var err error
	back := config()
	for {
		timeWait := back.NextBackOff()
		time.Sleep(timeWait)
		conn, err = db.GetConnection(host, port, user, dbname, password, sslmode)
		if err != nil {
			logrus.Error("we wait connect to postgres, time: ", timeWait)
		} else {
			break
		}
	}
	return conn, err
}

func config() *backoff.ExponentialBackOff {
	back := backoff.NewExponentialBackOff()
	back.MaxInterval = 20 * time.Second
	back.Multiplier = 2
	return back
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func NewResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{w, http.StatusOK}
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

var totalRequests = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Number of get requests.",
	},
	[]string{"path"},
)

var responseStatus = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "response_status",
		Help: "Status of HTTP response",
	},
	[]string{"status"},
)

var httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name: "http_response_time_seconds",
	Help: "Duration of HTTP requests.",
}, []string{"path"})

func prometheusMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := mux.CurrentRoute(r)
		path, _ := route.GetPathTemplate()

		timer := prometheus.NewTimer(httpDuration.WithLabelValues(path))
		rw := NewResponseWriter(w)
		next.ServeHTTP(rw, r)

		statusCode := rw.statusCode

		responseStatus.WithLabelValues(strconv.Itoa(statusCode)).Inc()
		totalRequests.WithLabelValues(path).Inc()

		timer.ObserveDuration()
	})
}
