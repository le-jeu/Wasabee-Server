package wasabeehttp

import (
	"context"
	"crypto/tls"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/oauth2"
	//"golang.org/x/oauth2/google"

	"github.com/gorilla/sessions"
	"github.com/wasabee-project/Wasabee-Server"

	// XXX gorilla has logging middleware, use that instead?
	"github.com/unrolled/logger"
)

// Configuration is the main configuration data for the http server
// an initial config is sent from main() and that is updated with defaults
// in the initializeConfig function
type Configuration struct {
	ListenHTTP       string
	FrontendPath     string
	Root             string
	path             string
	oauthStateString string
	CertDir          string
	OauthConfig      *oauth2.Config
	OauthUserInfoURL string
	store            *sessions.CookieStore
	sessionName      string
	CookieSessionKey string
	TemplateSet      map[string]*template.Template // allow multiple translations
	Logfile          string
	UseHTTPS         bool
	srv              *http.Server
	logfileHandle    *os.File
	unrolled         *logger.Logger
	scanners         map[string]int64
}

var config Configuration

const jsonType = "application/json; charset=UTF-8"
const jsonTypeShort = "application/json"
const jsonStatusOK = `{"status":"ok"}`
const jsonStatusEmpty = `{"status":"error","error":"Empty JSON"}`
const me = "/me"
const login = "/login"
const callback = "/callback"
const aptoken = "/aptok"
const apipath = "/api/v1"
const oneTimeToken = "/oneTimeToken"

// initializeConfig will normalize the options and create the "config" object.
func initializeConfig(initialConfig Configuration) {
	config = initialConfig

	config.Root = strings.TrimSuffix(config.Root, "/")

	// Extract "path" fron "root"
	rootParts := strings.SplitAfterN(config.Root, "/", 4) // https://example.org/[grab this part]
	config.path = ""
	if len(rootParts) > 3 { // Otherwise: application in root folder
		config.path = rootParts[3]
	}
	config.path = strings.TrimSuffix("/"+strings.TrimPrefix(config.path, "/"), "/")

	// used for templates
	wasabee.SetWebroot(config.Root)
	wasabee.SetWebAPIPath(apipath)

	if config.OauthConfig.ClientID == "" {
		wasabee.Log.Fatal("OAUTH_CLIENT_ID unset: logins will fail")
	}
	if config.OauthConfig.ClientSecret == "" {
		wasabee.Log.Fatal("OAUTH_SECRET unset: logins will fail")
	}

	wasabee.Log.Debugw("startup", "ClientID", config.OauthConfig.ClientID)
	wasabee.Log.Debugw("startup", "ClientSecret", config.OauthConfig.ClientSecret)
	config.oauthStateString = wasabee.GenerateName()
	wasabee.Log.Debugw("startup", "oauthStateString", config.oauthStateString)

	if config.CookieSessionKey == "" {
		wasabee.Log.Error("SESSION_KEY unset: logins will fail")
	} else {
		key := config.CookieSessionKey
		wasabee.Log.Debugw("startup", "Session Key", key)
		config.store = sessions.NewCookieStore([]byte(key))
		config.sessionName = "wasabee"
	}

	// certificate directory cleanup
	if config.CertDir == "" {
		wasabee.Log.Warn("CERTDIR unset: defaulting to 'certs'")
		config.CertDir = "certs"
	}
	certdir, err := filepath.Abs(config.CertDir)
	config.CertDir = certdir
	if err != nil {
		wasabee.Log.Fatal("certificate path could not be resolved.")
		// panic(err)
	}
	wasabee.Log.Debugw("startup", "Certificate Directory", config.CertDir)

	if config.Logfile == "" {
		config.Logfile = "wasabee-http.log"
	}
	wasabee.Log.Debugw("startup", "http logfile", config.Logfile)
	// #nosec
	config.logfileHandle, err = os.OpenFile(config.Logfile, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		wasabee.Log.Fatal(err)
	}
	config.unrolled = logger.New(logger.Options{
		Prefix: "wasabee",
		Out:    config.logfileHandle,
		IgnoredRequestURIs: []string{
			"/favicon.ico",
			"/apple-touch-icon-precomposed.png",
			"/apple-touch-icon-120x120-precomposed.png",
			"/apple-touch-icon-120x120.png",
			"/apple-touch-icon.png"},
	})
	config.scanners = make(map[string]int64)
}

// templateExecute outputs directly to the ResponseWriter
func templateExecute(res http.ResponseWriter, req *http.Request, name string, data interface{}) error {
	var lang string
	tmp := req.Header.Get("Accept-Language")
	if tmp == "" || len(tmp) < 2 {
		lang = "en"
	} else {
		lang = strings.ToLower(tmp)[:2]
	}
	_, ok := config.TemplateSet[lang]
	if !ok {
		lang = "en" // default to english if the map doesn't exist
	}

	if err := config.TemplateSet[lang].ExecuteTemplate(res, name, data); err != nil {
		wasabee.Log.Error(err)
		return err
	}
	return nil
}

// StartHTTP launches the HTTP server which is responsible for the frontend and the HTTP API.
func StartHTTP(initialConfig Configuration) {
	// take the incoming config, add defaults
	initializeConfig(initialConfig)

	// setup the main router an built-in subrouters
	router := setupRouter()

	// serve
	config.srv = &http.Server{
		Handler:           router,
		Addr:              config.ListenHTTP,
		WriteTimeout:      wasabee.GetTimeout(15 * time.Second),
		ReadTimeout:       wasabee.GetTimeout(15 * time.Second),
		ReadHeaderTimeout: wasabee.GetTimeout(2 * time.Second),
	}
	if config.UseHTTPS {
		config.srv.TLSConfig = &tls.Config{
			MinVersion:               tls.VersionTLS12,
			CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
			PreferServerCipherSuites: true,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
				tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_RSA_WITH_AES_256_CBC_SHA,
			},
		}

		wasabee.Log.Infow("startup", "port", config.ListenHTTP, "url", config.Root, "message", "online at "+config.Root)
		if err := config.srv.ListenAndServeTLS(config.CertDir+"/wasabee.fullchain.pem", config.CertDir+"/wasabee.key"); err != nil {
			wasabee.Log.Fatal(err)
		}
	} else {
		wasabee.Log.Infow("startup", "port", config.ListenHTTP, "url", config.Root, "message", "online at "+config.Root)
		if err := config.srv.ListenAndServe(); err != nil {
			wasabee.Log.Fatal(err)
		}
	}
}

// StartAppEngine is used in Google App Engine in place of StartHTTP
func StartAppEngine(ic Configuration) {
	initializeConfig(ic)

	router := setupRouter()
	config.srv = &http.Server{
		Handler: router,
		Addr:    config.ListenHTTP,
	}

	if err := config.srv.ListenAndServe(); err != nil {
		wasabee.Log.Fatal(err)
		// panic(err)
	}
}

// Shutdown forces a graceful shutdown of the http server
func Shutdown() error {
	wasabee.Log.Infow("shutdown", "message", "shutting down HTTP server")
	if err := config.srv.Shutdown(context.Background()); err != nil {
		wasabee.Log.Error(err)
		return err
	}
	return nil
}

func headersMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Add("Server", "Wasabee-Server")
		res.Header().Add("Content-Security-Policy", "frame-ancestors https://intel.ingress.com")
		res.Header().Add("X-Frame-Options", "allow-from https://intel.ingress.com") // deprecated
		res.Header().Add("Access-Control-Allow-Origin", "https://intel.ingress.com")
		res.Header().Add("Access-Control-Allow-Methods", "POST, GET, PUT, OPTIONS, HEAD, DELETE")
		res.Header().Add("Access-Control-Allow-Credentials", "true")
		res.Header().Add("Access-Control-Allow-Headers", "Content-Type, Accept, If-Modified-Since")
		next.ServeHTTP(res, req)
	})
}

func scannerMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		i, ok := config.scanners[req.RemoteAddr]
		if ok && i > 30 {
			http.Error(res, "scanner detected", http.StatusForbidden)
			return
		}
		next.ServeHTTP(res, req)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (rec *statusRecorder) WriteHeader(code int) {
	rec.status = code
	rec.ResponseWriter.WriteHeader(code)
}

func logRequestMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		wasabee.Log.Debug("REQ", req.Method, req.RequestURI)
		rec := statusRecorder{res, 200}
		next.ServeHTTP(&rec, req)
		wasabee.Log.Debug("RESP", rec.status, req.RequestURI)
	})
}

func authMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		ses, err := config.store.Get(req, config.sessionName)
		if err != nil {
			wasabee.Log.Error(err)
			delete(ses.Values, "nonce")
			delete(ses.Values, "id")
			delete(ses.Values, "loginReq")
			res.Header().Set("Connection", "close")
			_ = ses.Save(req, res)
			http.Error(res, err.Error(), http.StatusInternalServerError)
			return
		}
		ses.Options = &sessions.Options{
			Path:     "/",
			MaxAge:   86400, // 0,
			SameSite: http.SameSiteNoneMode,
			Secure:   true,
		}

		id, ok := ses.Values["id"]
		if !ok || id == nil {
			// XXX cookie and returnto may be redundant, but cookie wasn't working in early tests
			ses.Values["loginReq"] = req.URL.String()
			res.Header().Set("Connection", "close")
			_ = ses.Save(req, res)
			// wasabee.Log.Debug("not logged in")
			redirectOrError(res, req)
			return
		}

		gid := wasabee.GoogleID(id.(string))
		if gid.CheckLogout() {
			// wasabee.Log.Debugw("honoring previously requested logout", "GID", gid.String())
			/* delete(ses.Values, "nonce")
			delete(ses.Values, "id")
			ses.Options = &sessions.Options{
				Path:     "/",
				MaxAge:   -1,
				SameSite: http.SameSiteNoneMode,
				Secure:   true,
			}
			http.Redirect(res, req, "/", http.StatusFound)
			return */
		}

		in, ok := ses.Values["nonce"]
		if !ok || in == nil {
			wasabee.Log.Errorw("gid set, but no nonce", "GID", gid.String())
			redirectOrError(res, req)
			return
		}

		nonce, pNonce := calculateNonce(gid)
		if in.(string) != nonce {
			res.Header().Set("Connection", "close")
			if in.(string) != pNonce {
				// wasabee.Log.Debugw("session timed out", "GID", gid.String())
				ses.Values["nonce"] = "unset"
			} else {
				ses.Values["nonce"] = nonce
			}
			_ = ses.Save(req, res)
		}

		if ses.Values["nonce"] == "unset" {
			redirectOrError(res, req)
			return
		}
		next.ServeHTTP(res, req)
	})
}

func redirectOrError(res http.ResponseWriter, req *http.Request) {
	if strings.Contains(req.Referer(), "intel.ingress.com") {
		http.Error(res, "Unauthorized", http.StatusUnauthorized)
	} else {
		var redirectURL = login
		if req.URL.String()[:len(me)] != me {
			redirectURL = login + "?returnto=" + req.URL.String()
		}

		http.Redirect(res, req, redirectURL, http.StatusFound)
	}
}

func googleRoute(res http.ResponseWriter, req *http.Request) {
	ret := req.FormValue("returnto")

	ses, err := config.store.Get(req, config.sessionName)
	if err != nil {
		wasabee.Log.Error(err)
		http.Error(res, err.Error(), http.StatusInternalServerError)
		return
	}
	if ret != "" {
		ses.Values["loginReq"] = ret
	} else {
		ses.Values["loginReq"] = me
	}
	ses.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400, // 0,
		SameSite: http.SameSiteNoneMode,
		Secure:   true,
	}
	_ = ses.Save(req, res)

	// the server may have several names/ports ; redirect back to the one the user called
	oc := config.OauthConfig
	oc.RedirectURL = fmt.Sprintf("https://%s%s", req.Host, callback)
	url := oc.AuthCodeURL(config.oauthStateString)
	http.Redirect(res, req, url, http.StatusSeeOther)
}

func jsonError(e error) string {
	return fmt.Sprintf(`{"status":"error","error":"%s"}`, e.Error())
}

func debugMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		dump, _ := httputil.DumpRequest(req, false)
		wasabee.Log.Debug(string(dump))
		next.ServeHTTP(res, req)
	})
}

func contentTypeIs(req *http.Request, check string) bool {
	contentType := strings.Split(strings.Replace(req.Header.Get("Content-Type"), " ", "", -1), ";")[0]
	return strings.EqualFold(contentType, check)
}
