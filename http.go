package wasabee

import (
	"sync"

	"github.com/gorilla/mux"
)

var once sync.Once

// wasabeeHTTPConfig stores values from the http server which are used in templates
// to allow URL creation in other services (e.g. Telegram)
var wasabeeHTTPConfig struct {
	webroot string
	apipath string
	router  *mux.Router
}

// NewRouter creates the HTTP router
func NewRouter() *mux.Router {
	// http://marcio.io/2015/07/singleton-pattern-in-go/
	once.Do(func() {
		Log.Debugw("startup", "router", "main HTTP router")
		wasabeeHTTPConfig.router = mux.NewRouter()
	})
	return wasabeeHTTPConfig.router
}

// Subrouter creates a Gorilla subroute with a prefix
func Subrouter(prefix string) *mux.Router {
	Log.Debugw("startup", "router", prefix)
	if wasabeeHTTPConfig.router == nil {
		NewRouter()
	}

	sr := wasabeeHTTPConfig.router.PathPrefix(prefix).Subrouter()
	return sr
}

// SetWebroot is called at http startup
func SetWebroot(w string) {
	wasabeeHTTPConfig.webroot = w
}

// GetWebroot is called from templates
func GetWebroot() (string, error) {
	return wasabeeHTTPConfig.webroot, nil
}

// SetWebAPIPath is called at http startup
func SetWebAPIPath(a string) {
	wasabeeHTTPConfig.apipath = a
}

// GetWebAPIPath is called from templates
func GetWebAPIPath() (string, error) {
	return wasabeeHTTPConfig.apipath, nil
}
